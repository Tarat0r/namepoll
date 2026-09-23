# Namepoll

Namepoll е малка и уютна анкета за събиране на идеи за име от приятели и семейство. Формата, текстовете и цветове се настройват директно в Docker Compose.

## Възможности

- Няколко полета за предложения, с избираем брой задължителни отговори.
- Responsive дизайн с цветове, които можеш да променяш от YAML.
- Автоматично отваряне и затваряне на анкетата по зададени дати.
- Публична или защитена с токен статистика, с брой участници, таблица на всички участия и пълен преглед на всяка изпратена форма.
- Защита от повторно изпращане и ограничаване на твърде чести заявки.
- Локално съхранение в SQLite без външна база данни.
- Docker image

## Бързо стартиране с Docker Compose

Нужни са само Docker и Docker Compose.

1. Копирай примерния Compose файл:

   ```bash
   cp compose.example.yaml compose.yaml
   ```

2. Отвори `compose.yaml` и промени текстовете, датите, полета и цветове в `NAMEPOLL_CONFIG_YAML`.

3. Стартирай приложението:

   ```bash
   docker compose up -d --build
   ```

4. Отвори [http://localhost:8080](http://localhost:8080).

## Настройка на формата

Пълната конфигурация е в `environment.NAMEPOLL_CONFIG_YAML`:

```yaml
environment:
  NAMEPOLL_CONFIG_YAML: |
    title: "Кое име ти харесва?"
    description: "Сподели любимите си предложения."
    start_date: 2026-09-20T10:00:00+03:00
    end_date: 2026-09-25T23:59:59+03:00
    theme:
      background: "#fffdfd"
      surface: "#fff7fa"
      text: "#4b2b37"
      accent: "#d7829d"
      accent_strong: "#a84b6a"
      field: "#fffdfd"
      field_alternate: "#f8edf1"
    statistics:
      public_after_end: false
      access_token: "change-me"
    fields:
      - name: suggestions
        label: Предложения
        type: text
        count: 5
        required_count: 1
```

### Основни полета

| Поле | Описание |
| --- | --- |
| `title` | Заглавието на формата. |
| `description` | Краткият текст под заглавието. |
| `start_date` | Начало на анкетата в RFC 3339 формат. |
| `end_date` | Край на анкетата; трябва да е след `start_date`. |
| `theme` | Цветете на интерфейса като шестцифрени HEX стойности, например `#d7829d`. |
| `statistics.public_after_end` | Показва резултатите публично след края на анкетата. |
| `statistics.access_token` | Таен токен за достъп до резултатите по всяко време. |
| `fields[].name` | Вътрешно уникално име: малки латински букви, цифри и `_`; започва с буква. |
| `fields[].label` | Етикетът, който хората виждат. |
| `fields[].type` | В момента се поддържа `text`. |
| `fields[].count` | Общ брой показани полета. |
| `fields[].required_count` | Минимален брой задължително попълнени полета. |

Предложенията за име се приемат на кирилица. Позволено е и тире, но не в началото или края на името и не като двойно тире.

## Статистика

Ако е зададен `access_token`, резултатите са достъпни на:

```text
http://localhost:8080/poll/statistics?token=ТВОЯТ_ТОКЕН
```

Ако `public_after_end` е `true`, статистиката става публична след `end_date` и без токен. Не публикувай и не споделяй тайния токен.

## Данни и спиране

SQLite базата се пази в Docker volume `namepoll-data`. Обикновеното спиране не изтрива отговорите:

```bash
docker compose down
```

Внимание: `docker compose down --volumes` изтрива и volume-а с базата данни.

За да видиш логовете:

```bash
docker compose logs -f app
```

## Локална разработка

Нужен е Go 1.27.1. При локално стартиране приложението използва `forms/default.yaml`, ако `NAMEPOLL_CONFIG_YAML` не е зададена.

```bash
go run ./cmd/server
```

Проверки:

```bash
go test ./...
go vet ./...
docker compose -f compose.example.yaml config
```

## Reverse proxy

Ако приложението е зад reverse proxy, можеш да зададеш доверените proxy мрежи чрез `TRUSTED_PROXY_CIDRS`. Подай ги като списък от CIDR стойности, разделени със запетая.
