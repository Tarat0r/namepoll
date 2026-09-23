package main

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Tarat0r/namepoll/internal/database"
	sqlc "github.com/Tarat0r/namepoll/internal/database/sqlc"
	"github.com/Tarat0r/namepoll/internal/form"
	"github.com/Tarat0r/namepoll/internal/ratelimit"
)

func TestSubmissionCookieControlsPollFlow(t *testing.T) {
	h := newTestHandler(t)
	var logs bytes.Buffer
	h.logger = slog.New(slog.NewJSONHandler(&logs, nil))

	values := url.Values{
		"author_name": {"Ада"},
		"suggestions": {"Нова", "Лира"},
	}
	request := httptest.NewRequest(http.MethodPost, "/poll/suggestions", strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	h.suggestions(response, request)

	if response.Code != http.StatusSeeOther {
		t.Fatalf("submit status = %d, want %d; body: %s", response.Code, http.StatusSeeOther, response.Body.String())
	}
	if location := response.Header().Get("Location"); location != "/poll/thanks" {
		t.Fatalf("submit location = %q, want /poll/thanks", location)
	}

	var submissionCookie *http.Cookie
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == submissionCookieName {
			submissionCookie = cookie
			break
		}
	}
	if submissionCookie == nil {
		t.Fatal("submission response did not set a submission_token cookie")
	}
	if !submissionCookie.HttpOnly || submissionCookie.Path != "/poll" {
		t.Fatalf("unexpected cookie settings: %#v", submissionCookie)
	}
	if len(submissionCookie.Value) != 43 {
		t.Fatalf("opaque cookie length = %d, want 43", len(submissionCookie.Value))
	}
	digest := sha256.Sum256([]byte(submissionCookie.Value))
	storedSubmission, err := h.queries.GetSubmissionByTokenHash(
		request.Context(),
		sql.NullString{String: hex.EncodeToString(digest[:]), Valid: true},
	)
	if err != nil {
		t.Fatalf("read stored submission token hash: %v", err)
	}
	storedHash := storedSubmission.TokenHash.String
	if storedHash != hex.EncodeToString(digest[:]) || storedHash == submissionCookie.Value {
		t.Fatal("database did not store only the submission token hash")
	}
	for _, privateValue := range []string{"Ада", "Нова", "Лира"} {
		if strings.Contains(logs.String(), privateValue) {
			t.Fatalf("structured log contains submitted value %q", privateValue)
		}
	}

	request = httptest.NewRequest(http.MethodPost, "/poll/suggestions", strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response = httptest.NewRecorder()
	h.suggestions(response, request)
	if response.Code != http.StatusTooManyRequests {
		t.Fatalf("repeat submission status = %d, want %d", response.Code, http.StatusTooManyRequests)
	}

	request = httptest.NewRequest(http.MethodGet, "/poll", nil)
	request.AddCookie(submissionCookie)
	response = httptest.NewRecorder()
	h.poll(response, request)
	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/poll/thanks" {
		t.Fatalf("poll after submission = %d %q, want redirect to thanks", response.Code, response.Header().Get("Location"))
	}

	request = httptest.NewRequest(http.MethodGet, "/poll/thanks", nil)
	response = httptest.NewRecorder()
	h.thanks(response, request)
	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/poll" {
		t.Fatalf("thanks without cookie = %d %q, want redirect to poll", response.Code, response.Header().Get("Location"))
	}

	request = httptest.NewRequest(http.MethodGet, "/poll", nil)
	request.AddCookie(&http.Cookie{Name: submissionCookieName, Value: strings.Repeat("A", 43)})
	response = httptest.NewRecorder()
	h.poll(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("poll with forged opaque cookie = %d, want %d", response.Code, http.StatusOK)
	}
}

func TestFinishedPollRedirectsToStatistics(t *testing.T) {
	h := newTestHandler(t)
	h.definition.EndDate = time.Now().Add(-time.Minute)

	for _, path := range []string{"/poll", "/poll/thanks"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		response := httptest.NewRecorder()
		if path == "/poll" {
			h.poll(response, request)
		} else {
			h.thanks(response, request)
		}
		if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/poll/statistics" {
			t.Fatalf("%s after finish = %d %q, want statistics redirect", path, response.Code, response.Header().Get("Location"))
		}
	}

	request := httptest.NewRequest(http.MethodGet, "/poll/statistics", nil)
	response := httptest.NewRecorder()
	h.statistics(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("statistics status = %d, want %d", response.Code, http.StatusOK)
	}
}

func TestStatisticsTokenAllowsAccessAtAnyTime(t *testing.T) {
	h := newTestHandler(t)
	h.definition.Statistics.AccessToken = "secret-token"

	tests := []struct {
		name       string
		target     string
		wantStatus int
	}{
		{name: "missing token", target: "/poll/statistics", wantStatus: http.StatusForbidden},
		{name: "wrong token", target: "/poll/statistics?token=wrong", wantStatus: http.StatusForbidden},
		{name: "valid token", target: "/poll/statistics?token=secret-token", wantStatus: http.StatusOK},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, test.target, nil)
			response := httptest.NewRecorder()
			h.statistics(response, request)
			if response.Code != test.wantStatus {
				t.Fatalf("statistics status = %d, want %d", response.Code, test.wantStatus)
			}
		})
	}
}

func TestStatisticsShowsWhoVotedForSuggestion(t *testing.T) {
	h := newTestHandler(t)
	h.definition.Statistics.AccessToken = "secret-token"

	submission, err := h.queries.CreateSubmission(t.Context(), sqlc.CreateSubmissionParams{
		AuthorName: sql.NullString{String: "Мария", Valid: true},
	})
	if err != nil {
		t.Fatalf("create submission: %v", err)
	}
	if _, err := h.queries.CreateSuggestion(t.Context(), sqlc.CreateSuggestionParams{
		SubmissionID: submission.ID,
		FieldName:    "suggestions",
		Name:         "Елена",
	}); err != nil {
		t.Fatalf("create suggestion: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/poll/statistics?token=secret-token", nil)
	response := httptest.NewRecorder()
	h.statistics(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("statistics status = %d, want %d", response.Code, http.StatusOK)
	}
	body := response.Body.String()
	for _, expected := range []string{
		"<details",
		"Елена",
		"Гласували:",
		"Мария",
		`href="/submissions?token=secret-token"`,
		fmt.Sprintf(`href="/submissions/%d?token=secret-token"`, submission.ID),
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("statistics response does not contain %q", expected)
		}
	}

	request = httptest.NewRequest(http.MethodGet, "/submissions?token=secret-token", nil)
	response = httptest.NewRecorder()
	h.submissions(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("submissions status = %d, want %d", response.Code, http.StatusOK)
	}
	for _, expected := range []string{"<table", "Мария", "Елена"} {
		if !strings.Contains(response.Body.String(), expected) {
			t.Fatalf("submissions response does not contain %q", expected)
		}
	}

	request = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/submissions/%d?token=secret-token", submission.ID), nil)
	request.SetPathValue("id", fmt.Sprintf("%d", submission.ID))
	response = httptest.NewRecorder()
	h.submission(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("submission status = %d, want %d", response.Code, http.StatusOK)
	}
	for _, expected := range []string{"Пълно участие", "Мария", "Елена"} {
		if !strings.Contains(response.Body.String(), expected) {
			t.Fatalf("submission response does not contain %q", expected)
		}
	}
}

func TestSubmissionsRequireStatisticsAccess(t *testing.T) {
	h := newTestHandler(t)
	h.definition.Statistics.AccessToken = "secret-token"

	for _, handler := range []struct {
		name   string
		target string
		run    func(http.ResponseWriter, *http.Request)
	}{
		{name: "list", target: "/submissions", run: h.submissions},
		{name: "detail", target: "/submissions/1", run: h.submission},
	} {
		t.Run(handler.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, handler.target, nil)
			response := httptest.NewRecorder()
			handler.run(response, request)
			if response.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
			}
		})
	}
}

func TestTokenCanBeRequiredAfterPollEnds(t *testing.T) {
	h := newTestHandler(t)
	h.definition.EndDate = time.Now().Add(-time.Minute)
	h.definition.Statistics.PublicAfterEnd = false
	h.definition.Statistics.AccessToken = "secret-token"

	request := httptest.NewRequest(http.MethodGet, "/poll/statistics", nil)
	response := httptest.NewRecorder()
	h.statistics(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("token-only statistics without token = %d, want %d", response.Code, http.StatusForbidden)
	}

	request = httptest.NewRequest(http.MethodGet, "/poll/statistics?token=secret-token", nil)
	response = httptest.NewRecorder()
	h.statistics(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("token-only statistics with token = %d, want %d", response.Code, http.StatusOK)
	}
}

func TestThanksMessageMatchesStatisticsConfiguration(t *testing.T) {
	h := newTestHandler(t)
	tests := []struct {
		name       string
		statistics form.StatisticsConfig
		wantText   string
	}{
		{
			name:       "public after end",
			statistics: form.StatisticsConfig{PublicAfterEnd: true},
			wantText:   "ще бъдат показани автоматично",
		},
		{
			name:       "token only",
			statistics: form.StatisticsConfig{AccessToken: "secret-token"},
			wantText:   "Предложенията ти бяха изпратени успешно",
		},
		{
			name:       "private",
			statistics: form.StatisticsConfig{},
			wantText:   "Предложенията ти бяха изпратени успешно",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			h.definition.Statistics = test.statistics
			var rendered bytes.Buffer
			if err := h.template.ExecuteTemplate(&rendered, "thanks.html", h.definition); err != nil {
				t.Fatalf("render thanks template: %v", err)
			}
			if !strings.Contains(rendered.String(), test.wantText) {
				t.Fatalf("thanks message does not contain %q", test.wantText)
			}
			if !test.statistics.PublicAfterEnd && strings.Contains(rendered.String(), `class="state-note"`) {
				t.Fatal("non-public thanks message should not show a statistics note")
			}
		})
	}
}

func TestRequiredSuggestionCount(t *testing.T) {
	h := newTestHandler(t)
	h.definition.Fields = []form.Field{
		{Name: "suggestions", Label: "Предложения", Type: "text", Count: 5, RequiredCount: 2},
	}

	response := httptest.NewRecorder()
	h.renderPoll(response, http.StatusOK, nil, "", "", "")
	if count := strings.Count(response.Body.String(), "required"); count != 2 {
		t.Fatalf("required inputs = %d, want 2", count)
	}

	values := url.Values{"suggestions": {"Нова"}}
	request := httptest.NewRequest(http.MethodPost, "/poll/suggestions", strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response = httptest.NewRecorder()
	h.suggestions(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("one suggestion status = %d, want %d", response.Code, http.StatusBadRequest)
	}

	values.Add("suggestions", "Лира")
	request = httptest.NewRequest(http.MethodPost, "/poll/suggestions", strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response = httptest.NewRecorder()
	h.suggestions(response, request)
	if response.Code != http.StatusSeeOther {
		t.Fatalf("two suggestions status = %d, want %d; body: %s", response.Code, http.StatusSeeOther, response.Body.String())
	}
}

func TestThemeRendersCSSVariables(t *testing.T) {
	h := newTestHandler(t)
	h.definition.Theme = form.ThemeConfig{
		Background: "#123456",
		Accent:     "#abcdef",
	}

	var rendered bytes.Buffer
	if err := h.template.ExecuteTemplate(&rendered, "thanks.html", h.definition); err != nil {
		t.Fatalf("render thanks template: %v", err)
	}

	body := rendered.String()
	for _, want := range []string{
		"--theme-background: #123456;",
		"--theme-accent: #abcdef;",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("rendered theme does not contain %q", want)
		}
	}
}

func TestValidationPreservesSubmittedValues(t *testing.T) {
	h := newTestHandler(t)
	h.definition.Fields[0].RequiredCount = 2

	values := url.Values{
		"author_name": {"Ада"},
		"suggestions": {"Нова", "Invalid123"},
	}
	request := httptest.NewRequest(http.MethodPost, "/poll/suggestions", strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	h.suggestions(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid submission status = %d, want %d", response.Code, http.StatusBadRequest)
	}
	for _, preserved := range []string{`value="Ада"`, `value="Нова"`, `value="Invalid123"`} {
		if !strings.Contains(response.Body.String(), preserved) {
			t.Fatalf("validation response does not preserve %s", preserved)
		}
	}
	if !strings.Contains(response.Body.String(), `role="alert"`) {
		t.Fatal("validation response has no accessible error alert")
	}
}

func TestGenericFieldsAreStoredSeparately(t *testing.T) {
	h := newTestHandler(t)
	h.definition.Fields = []form.Field{
		{Name: "first_names", Label: "Първи имена", Type: "text", Count: 2, RequiredCount: 1},
		{Name: "middle_names", Label: "Бащини имена", Type: "text", Count: 2, RequiredCount: 1},
	}

	values := url.Values{
		"first_names":  {"Нова"},
		"middle_names": {"Нова"},
	}
	request := httptest.NewRequest(http.MethodPost, "/poll/suggestions", strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	h.suggestions(response, request)
	if response.Code != http.StatusSeeOther {
		t.Fatalf("generic submission status = %d, want %d; body: %s", response.Code, http.StatusSeeOther, response.Body.String())
	}

	rows, err := h.queries.ListSuggestions(request.Context())
	if err != nil {
		t.Fatalf("list generic suggestions: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("stored generic values = %d, want 2", len(rows))
	}
	fields := map[string]bool{}
	for _, row := range rows {
		fields[row.FieldName] = true
	}
	if !fields["first_names"] || !fields["middle_names"] {
		t.Fatalf("stored field names = %#v", fields)
	}
}

func TestPollStatePages(t *testing.T) {
	t.Run("scheduled", func(t *testing.T) {
		h := newTestHandler(t)
		h.definition.StartDate = time.Now().Add(time.Hour)
		h.definition.EndDate = time.Now().Add(2 * time.Hour)

		request := httptest.NewRequest(http.MethodGet, "/poll", nil)
		response := httptest.NewRecorder()
		h.poll(response, request)
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "все още не е започнала") {
			t.Fatalf("scheduled poll response = %d %q", response.Code, response.Body.String())
		}

		request = httptest.NewRequest(http.MethodPost, "/poll/suggestions", nil)
		response = httptest.NewRecorder()
		h.suggestions(response, request)
		if response.Code != http.StatusForbidden {
			t.Fatalf("scheduled submission status = %d, want %d", response.Code, http.StatusForbidden)
		}
	})

	t.Run("finished private", func(t *testing.T) {
		h := newTestHandler(t)
		h.definition.EndDate = time.Now().Add(-time.Minute)
		h.definition.Statistics.PublicAfterEnd = false

		request := httptest.NewRequest(http.MethodGet, "/poll", nil)
		response := httptest.NewRecorder()
		h.poll(response, request)
		if response.Code != http.StatusGone || !strings.Contains(response.Body.String(), "Анкетата приключи") {
			t.Fatalf("finished private poll response = %d %q", response.Code, response.Body.String())
		}
	})
}

func newTestHandler(t *testing.T) Handler {
	t.Helper()

	db, err := database.Open(filepath.Join(t.TempDir(), "namepoll.db"))
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	templates, err := template.ParseGlob(filepath.Join("..", "..", "web", "templates", "*.html"))
	if err != nil {
		t.Fatalf("parse templates: %v", err)
	}

	now := time.Now()
	return Handler{
		template: templates,
		definition: &form.Definition{
			Title:     "Name poll",
			StartDate: now.Add(-time.Hour),
			EndDate:   now.Add(time.Hour),
			Statistics: form.StatisticsConfig{
				PublicAfterEnd: true,
			},
			Fields: []form.Field{
				{Name: "suggestions", Label: "Предложения", Type: "text", Count: 5, RequiredCount: 1},
			},
		},
		db:      db,
		queries: sqlc.New(db),
		limiter: ratelimit.New(db, 30*time.Second),
		logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}
