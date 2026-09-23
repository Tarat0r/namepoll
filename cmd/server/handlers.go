package main

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	sqlc "github.com/Tarat0r/namepoll/internal/database/sqlc"
	"github.com/Tarat0r/namepoll/internal/form"
	"github.com/Tarat0r/namepoll/internal/ratelimit"
)

const (
	submissionCookieName = "submission_token"
	maxFormBodyBytes     = 64 << 10
)

type Handler struct {
	template   *template.Template
	definition *form.Definition
	db         *sql.DB
	queries    *sqlc.Queries
	limiter    *ratelimit.Limiter
	clientIPs  *ratelimit.ClientIPResolver
	logger     *slog.Logger
	now        func() time.Time
}

type pollPage struct {
	Definition  *form.Definition
	AuthorName  string
	AuthorError string
	Fields      []pollFieldPage
	Error       string
}

type pollFieldPage struct {
	Name          string
	Label         string
	RequiredCount uint
	Inputs        []pollInput
	Error         string
}

type pollInput struct {
	ID       string
	Number   int
	Value    string
	Required bool
}

type statePage struct {
	Definition *form.Definition
}

type statisticItem struct {
	Name   string
	Count  int64
	Voters []string
}

type statisticGroup struct {
	Label string
	Items []statisticItem
}

type statisticsPage struct {
	Definition *form.Definition
	Groups     []statisticGroup
}

type pendingSuggestion struct {
	fieldName string
	name      string
}

func (h Handler) poll(w http.ResponseWriter, r *http.Request) {
	switch h.definition.Status(h.clock()) {
	case form.StatusScheduled:
		h.renderState(w, http.StatusOK, "scheduled.html")
		return
	case form.StatusFinished:
		if h.definition.Statistics.PublicAfterEnd {
			http.Redirect(w, r, "/poll/statistics", http.StatusSeeOther)
			return
		}
		h.renderState(w, http.StatusGone, "finished.html")
		return
	}

	submitted, err := h.hasSubmitted(r)
	if err != nil {
		h.log().Error("submission cookie lookup failed", "error", err)
		http.Error(w, "Неуспешна проверка на предложенията.", http.StatusInternalServerError)
		return
	}
	if submitted {
		http.Redirect(w, r, "/poll/thanks", http.StatusSeeOther)
		return
	}

	h.renderPoll(w, http.StatusOK, nil, "", "", "")
}

func (h Handler) suggestions(w http.ResponseWriter, r *http.Request) {
	switch h.definition.Status(h.clock()) {
	case form.StatusScheduled:
		h.renderState(w, http.StatusForbidden, "scheduled.html")
		return
	case form.StatusFinished:
		if h.definition.Statistics.PublicAfterEnd {
			http.Redirect(w, r, "/poll/statistics", http.StatusSeeOther)
			return
		}
		h.renderState(w, http.StatusGone, "finished.html")
		return
	}

	submitted, err := h.hasSubmitted(r)
	if err != nil {
		h.log().Error("submission cookie lookup failed", "error", err)
		http.Error(w, "Неуспешна проверка на предложенията.", http.StatusInternalServerError)
		return
	}
	if submitted {
		http.Redirect(w, r, "/poll/thanks", http.StatusSeeOther)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil {
		h.renderPoll(w, http.StatusBadRequest, nil, "", "", "Невалидни или прекалено много данни.")
		return
	}

	author, authorError, values, fieldErrors, suggestions := h.validateSubmission(r.PostForm)
	if authorError != "" || len(fieldErrors) > 0 {
		h.renderPoll(
			w,
			http.StatusBadRequest,
			values,
			r.PostFormValue("author_name"),
			authorError,
			"Проверете посочените полета.",
			fieldErrors,
		)
		return
	}

	clientKey := r.RemoteAddr
	if h.clientIPs != nil {
		clientKey = h.clientIPs.ClientIP(r)
	}
	allowed, err := h.limiter.Allow(r.Context(), clientKey)
	if err != nil {
		h.log().Error("rate-limit check failed", "error", err)
		http.Error(w, "Предложенията не бяха изпратени.", http.StatusInternalServerError)
		return
	}
	if !allowed {
		h.log().Info("submission rate limited")
		http.Error(w, "Твърде много опити. Опитайте отново по-късно.", http.StatusTooManyRequests)
		return
	}

	rawToken, tokenHash, err := newSubmissionToken()
	if err != nil {
		h.log().Error("submission token generation failed", "error", err)
		http.Error(w, "Предложенията не бяха изпратени.", http.StatusInternalServerError)
		return
	}

	tx, err := h.db.BeginTx(r.Context(), nil)
	if err != nil {
		h.log().Error("submission transaction start failed", "error", err)
		http.Error(w, "Предложенията не бяха изпратени.", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	qtx := h.queries.WithTx(tx)
	submission, err := qtx.CreateSubmission(r.Context(), sqlc.CreateSubmissionParams{
		TokenHash:  sql.NullString{String: tokenHash, Valid: true},
		AuthorName: sql.NullString{String: author, Valid: author != ""},
	})
	if err != nil {
		h.log().Error("submission insert failed", "error", err)
		http.Error(w, "Предложенията не бяха изпратени.", http.StatusInternalServerError)
		return
	}

	for _, suggestion := range suggestions {
		if _, err := qtx.CreateSuggestion(r.Context(), sqlc.CreateSuggestionParams{
			SubmissionID: submission.ID,
			FieldName:    suggestion.fieldName,
			Name:         suggestion.name,
		}); err != nil {
			h.log().Error("suggestion insert failed", "error", err, "field", suggestion.fieldName)
			http.Error(w, "Предложенията не бяха изпратени.", http.StatusInternalServerError)
			return
		}
	}

	if err := tx.Commit(); err != nil {
		h.log().Error("submission transaction commit failed", "error", err)
		http.Error(w, "Предложенията не бяха изпратени.", http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     submissionCookieName,
		Value:    rawToken,
		Path:     "/poll",
		Expires:  h.definition.EndDate,
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
	})
	h.log().Info("submission accepted", "submission_id", submission.ID, "field_value_count", len(suggestions))
	http.Redirect(w, r, "/poll/thanks", http.StatusSeeOther)
}

func (h Handler) validateSubmission(postForm url.Values) (
	author string,
	authorError string,
	values url.Values,
	fieldErrors map[string]string,
	suggestions []pendingSuggestion,
) {
	values = postForm.Clone()
	fieldErrors = make(map[string]string)
	author = strings.TrimSpace(postForm.Get("author_name"))
	if author != "" {
		normalized, err := form.NormalizeName(author)
		if err != nil {
			authorError = "Използвай само букви на кирилица и тире."
		} else {
			author = normalized
		}
	}

	for _, field := range h.definition.Fields {
		seen := make(map[string]struct{})
		hadInvalidValue := false
		rawValues := postForm[field.Name]
		tooManyValues := len(rawValues) > int(field.Count)
		if tooManyValues {
			rawValues = rawValues[:field.Count]
		}
		for _, raw := range rawValues {
			if strings.TrimSpace(raw) == "" {
				continue
			}
			normalized, err := form.NormalizeName(raw)
			if err != nil {
				hadInvalidValue = true
				continue
			}
			if _, duplicate := seen[normalized]; duplicate {
				continue
			}
			seen[normalized] = struct{}{}
			suggestions = append(suggestions, pendingSuggestion{
				fieldName: field.Name,
				name:      normalized,
			})
		}

		switch {
		case tooManyValues:
			fieldErrors[field.Name] = "Изпратени са твърде много стойности за това поле."
		case hadInvalidValue:
			fieldErrors[field.Name] = "Използвай само букви на кирилица и тире."
		case uint(len(seen)) < field.RequiredCount:
			fieldErrors[field.Name] = fmt.Sprintf(
				"Попълни поне %d различни предложения.",
				field.RequiredCount,
			)
		}
	}

	return author, authorError, values, fieldErrors, suggestions
}

func (h Handler) submissions(w http.ResponseWriter, r *http.Request) {
	submissionCount, err := h.queries.CountSubmissions(r.Context())
	if err != nil {
		h.log().Error("submission count failed", "error", err)
		http.Error(w, "Предложенията не може да бъдат заредени.", http.StatusInternalServerError)
		return
	}
	suggestionCount, err := h.queries.CountSuggestions(r.Context())
	if err != nil {
		h.log().Error("suggestion count failed", "error", err)
		http.Error(w, "Предложенията не може да бъдат заредени.", http.StatusInternalServerError)
		return
	}

	h.log().Info("submission totals requested", "submissions", submissionCount, "field_values", suggestionCount)
	fmt.Fprintf(w, "Изпращания: %d; попълнени полета: %d\n", submissionCount, suggestionCount)
}

func (h Handler) thanks(w http.ResponseWriter, r *http.Request) {
	if h.definition.Status(h.clock()) == form.StatusFinished {
		if h.definition.Statistics.PublicAfterEnd {
			http.Redirect(w, r, "/poll/statistics", http.StatusSeeOther)
			return
		}
		h.renderState(w, http.StatusGone, "finished.html")
		return
	}

	submitted, err := h.hasSubmitted(r)
	if err != nil {
		h.log().Error("submission cookie lookup failed", "error", err)
		http.Error(w, "Неуспешна проверка на предложенията.", http.StatusInternalServerError)
		return
	}
	if !submitted {
		http.Redirect(w, r, "/poll", http.StatusSeeOther)
		return
	}

	if err := h.render(w, http.StatusOK, "thanks.html", h.definition); err != nil {
		h.renderError(w, "thanks", err)
	}
}

func (h Handler) statistics(w http.ResponseWriter, r *http.Request) {
	if !h.canViewStatistics(r) {
		http.Error(w, "Статистиката не е достъпна без валиден токен.", http.StatusForbidden)
		return
	}

	stats, err := h.queries.ListSuggestionStats(r.Context())
	if err != nil {
		h.log().Error("statistics load failed", "error", err)
		http.Error(w, "Статистиката не може да бъде заредена.", http.StatusInternalServerError)
		return
	}

	voters, err := h.queries.ListSuggestionVoters(r.Context())
	if err != nil {
		h.log().Error("statistics voters load failed", "error", err)
		http.Error(w, "Статистиката не може да бъде заредена.", http.StatusInternalServerError)
		return
	}

	groups := h.groupStatistics(stats, voters)
	data := statisticsPage{Definition: h.definition, Groups: groups}
	if err := h.render(w, http.StatusOK, "statistics.html", data); err != nil {
		h.renderError(w, "statistics", err)
	}
}

func (h Handler) groupStatistics(rows []sqlc.ListSuggestionStatsRow, voterRows []sqlc.ListSuggestionVotersRow) []statisticGroup {
	votersByField := make(map[string]map[string][]string)
	for _, row := range voterRows {
		if votersByField[row.FieldName] == nil {
			votersByField[row.FieldName] = make(map[string][]string)
		}

		voter := "Анонимен"
		if row.AuthorName.Valid && strings.TrimSpace(row.AuthorName.String) != "" {
			voter = row.AuthorName.String
		}
		votersByField[row.FieldName][row.Name] = append(votersByField[row.FieldName][row.Name], voter)
	}

	byField := make(map[string][]statisticItem)
	for _, row := range rows {
		byField[row.FieldName] = append(byField[row.FieldName], statisticItem{
			Name:   row.Name,
			Count:  row.SuggestionCount,
			Voters: votersByField[row.FieldName][row.Name],
		})
	}

	groups := make([]statisticGroup, 0, len(byField))
	for _, field := range h.definition.Fields {
		items := byField[field.Name]
		if len(items) == 0 {
			continue
		}
		groups = append(groups, statisticGroup{Label: field.Label, Items: items})
		delete(byField, field.Name)
	}
	for fieldName, items := range byField {
		groups = append(groups, statisticGroup{Label: fieldName, Items: items})
	}
	return groups
}

func (h Handler) canViewStatistics(r *http.Request) bool {
	if h.definition.Statistics.PublicAfterEnd &&
		h.definition.Status(h.clock()) == form.StatusFinished {
		return true
	}

	expected := h.definition.Statistics.AccessToken
	provided := r.URL.Query().Get("token")
	if expected == "" || provided == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) == 1
}

func (h Handler) hasSubmitted(r *http.Request) (bool, error) {
	cookie, err := r.Cookie(submissionCookieName)
	if errors.Is(err, http.ErrNoCookie) || err != nil {
		return false, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(cookie.Value)
	if err != nil || len(decoded) != 32 {
		return false, nil
	}

	hash := sha256.Sum256([]byte(cookie.Value))
	_, err = h.queries.GetSubmissionByTokenHash(
		r.Context(),
		sql.NullString{String: hex.EncodeToString(hash[:]), Valid: true},
	)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func newSubmissionToken() (raw string, hash string, err error) {
	buffer := make([]byte, 32)
	if _, err := rand.Read(buffer); err != nil {
		return "", "", err
	}
	raw = base64.RawURLEncoding.EncodeToString(buffer)
	digest := sha256.Sum256([]byte(raw))
	return raw, hex.EncodeToString(digest[:]), nil
}

func (h Handler) renderPoll(
	w http.ResponseWriter,
	status int,
	values url.Values,
	authorName string,
	authorError string,
	formError string,
	fieldErrors ...map[string]string,
) {
	errorsByField := map[string]string{}
	if len(fieldErrors) > 0 && fieldErrors[0] != nil {
		errorsByField = fieldErrors[0]
	}
	page := pollPage{
		Definition:  h.definition,
		AuthorName:  authorName,
		AuthorError: authorError,
		Error:       formError,
	}
	for _, field := range h.definition.Fields {
		fieldPage := pollFieldPage{
			Name:          field.Name,
			Label:         field.Label,
			RequiredCount: field.RequiredCount,
			Error:         errorsByField[field.Name],
			Inputs:        make([]pollInput, field.Count),
		}
		for index := range fieldPage.Inputs {
			value := ""
			if values != nil && index < len(values[field.Name]) {
				value = values[field.Name][index]
			}
			fieldPage.Inputs[index] = pollInput{
				ID:       fmt.Sprintf("%s_%d", field.Name, index+1),
				Number:   index + 1,
				Value:    value,
				Required: uint(index) < field.RequiredCount,
			}
		}
		page.Fields = append(page.Fields, fieldPage)
	}

	if err := h.render(w, status, "poll.html", page); err != nil {
		h.renderError(w, "poll", err)
	}
}

func (h Handler) renderState(w http.ResponseWriter, status int, templateName string) {
	if err := h.render(w, status, templateName, statePage{Definition: h.definition}); err != nil {
		h.renderError(w, templateName, err)
	}
}

func (h Handler) render(w http.ResponseWriter, status int, name string, data any) error {
	var output bytes.Buffer
	if err := h.template.ExecuteTemplate(&output, name, data); err != nil {
		return err
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if _, err := output.WriteTo(w); err != nil {
		h.log().Error("response write failed", "page", name, "error", err)
	}
	return nil
}

func (h Handler) renderError(w http.ResponseWriter, page string, err error) {
	h.log().Error("template render failed", "page", page, "error", err)
	http.Error(w, "Страницата не може да бъде показана.", http.StatusInternalServerError)
}

func (h Handler) clock() time.Time {
	if h.now != nil {
		return h.now()
	}
	return time.Now()
}

func (h Handler) log() *slog.Logger {
	if h.logger != nil {
		return h.logger
	}
	return slog.Default()
}
