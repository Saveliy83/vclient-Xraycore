# Итоговый отчёт аудита

Дата: 2026-10-12  
Проверяемая зависимость: `github.com/xtls/xray-core v1.260327.0`  
Версия Go по `go.mod`: `1.27.2`

## Результаты проверок

| Проверка | Результат |
| --- | --- |
| `go vet ./...` | Прошла |
| `gofmt -l .` | Пустой вывод |
| `go test -race ./...` | Не прошла локально: с `CGO_ENABLED=1` cgo сообщает `C compiler "gcc" not found`. CI включает CGO и устанавливает GCC для Windows; результат GitHub Actions необходимо проверить после push. |
| `go test -cover ./...` | `internal/config` — 82.0%, `internal/xrayconf` — 81.8%; оба выше цели 70%. Остальные пакеты: cmd/vclient 20.9%, internal/core 25.9%, internal/dns 66.1%, internal/logger 76.2%, internal/reload 57.1%, internal/router 73.3%. |
| `golangci-lint run` | Прошла, `0 issues` (golangci-lint v2.14.0). |

`go test -race ./...` из CI выполняется и на Linux, и на Windows; Windows job
устанавливает MinGW GCC. Локальный `go test -race` не объявляется успешным.

## Проверка безопасности

- Журналы приложения не содержат UUID, REALITY `pbk` или текст подписки:
  стартовое сообщение пишет транспорт/адрес/listen, ошибки парсинга подписки
  содержат номер строки и ошибку, а содержимое ответа не логируется.
  У Xray задан уровень `warning`; аргументы профиля в debug-сообщения приложения
  не передаются.
- Кэш подписки создаётся через `os.CreateTemp`, который на POSIX даёт режим
  `0600`; на Windows к файлу устанавливается защищённая DACL только текущего
  пользователя. Добавлены ограничение размера кэша (10 MiB) и проверка
  сериализуемых данных.
- HTTP подписка ограничена 30-секундным context timeout и телом 5 MiB.
  Windows reload HTTP server имеет timeout чтения/записи, лимит заголовков и
  слушает только loopback.
- Найденные адреса `127.0.0.1:1080/1081` — локальные значения по умолчанию,
  `127.0.0.1:9090` — фиксированный локальный reload endpoint. UUID и REALITY
  ключи в fixtures/golden-файлах — синтетические тестовые данные, не ключи
  реальных серверов.

## Исправления аудита

| Файл | Строка после изменений | Проблема | Исправление |
| --- | ---: | --- | --- |
| `cmd/vclient/reload_endpoint_windows.go` | 22 | У локального HTTP сервера отсутствовали явные ограничения времени и размера заголовков. | Установлены `ReadHeaderTimeout`, `ReadTimeout`, `WriteTimeout`, `IdleTimeout` и `MaxHeaderBytes`. |
| `internal/config/subscription.go` | 189–203 | Кэш читался целиком через `os.ReadFile` без верхней границы размера. | Добавлено ограничение кэша 10 MiB на запись и чтение. |
| `internal/config/cache_permissions_windows.go` | 11 | Windows DACL не ограничивала доступ к кэш-файлу текущим пользователем. | Файл получает отдельную защищённую DACL с ACE только для SID текущего пользователя; добавлен Windows-тест проверки ACL. |
| `internal/reload/controller.go` | 79 | Успешный ответ endpoint игнорировал ошибку записи тела. | Успешный ответ теперь только HTTP 200 без тела. |

## Оставшиеся проблемы

| Файл | Строка | Описание | Исправление |
| --- | ---: | --- | --- |
| `cmd/vclient/main.go` | 50–52 | `-h` завершает CLI с `flag: help requested`, справка скрыта. | Обработать `flag.ErrHelp` и вывести `FlagSet` в stdout; см. [known-issues.md](./known-issues.md). |
## CI и повторение проверок

Workflow: [`.github/workflows/ci.yml`](../.github/workflows/ci.yml). Он запускает
build, vet, race tests и golangci-lint на Windows и Linux. Windows job включает
MinGW для race detector.

```powershell
go vet ./...
gofmt -l .
go test -race ./...
go test -cover ./...
golangci-lint run
```

Локальный build helper:

```powershell
.\build.ps1 build
.\build.ps1 test
.\build.ps1 lint
.\build.ps1 fuzz
.\build.ps1 release -Version 0.1.0
```

В текущей среде build, `go vet`, `gofmt`, покрытие, unit-тесты и golangci-lint
выполнены успешно. Race detector локально недоступен без C-компилятора.
Проверьте результат CI после push, включая выполнение Windows ACL теста.
