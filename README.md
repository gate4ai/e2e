# gate4ai e2e

Сквозные тесты [gate4ai](https://github.com/gate4ai/gate4ai): хост собирается из исходников, официальные коннекторы ставятся как свои сборки, `daemon` и CLI работают по-настоящему. Фейковые здесь только внешние серверы: IMAP/SMTP (`gate4ai-mail/mailtest`) и Telegram Bot API.

| Тест | Что проверяет |
|---|---|
| `TestReadmeFlow` | поток из README: `send_email` → ask → подтверждение в Telegram → SMTP → проверка по «Отправленным» → квитанция; отказ по политике; экспорт журнала |
| `TestQuickstartFlow` | конфигурация из `docs/quickstart.md` принимается с коннекторами mail, files, browser; `files.append` по правилу из неё, чтение вне корней запрещено |

## Запуск

Репозитории лежат рядом, как в общем каталоге `gate4ai/`; [go.work](go.work) связывает их, поэтому проверяются соседние исходники, а не опубликованные версии модулей.

```
gate4ai/
  e2e/  gate4ai/  sdk-go/  relay/
  connectors/mail/  connectors/files/  connectors/browser/
```

```sh
go test ./...          # около минуты; -short пропускает всё
```

## CI

[e2e.yml](.github/workflows/e2e.yml) — переиспользуемый workflow. Ключевые репозитории вызывают его из своего `ci.yml` со своим коммитом; он берёт вызвавший репозиторий на этом коммите, остальные на `main`. Так изменение в любом из них проверяется вместе с остальными ещё в pull request.

Направление зависимостей: e2e зависит от всех, от e2e — никто. Хост не импортирует коннекторы даже в тестах.

## Лицензия

[AGPL-3.0-only](LICENSE): тесты импортируют `gate4ai-mail/mailtest`. Вклад — по DCO (`git commit -s`).
