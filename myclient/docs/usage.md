# Использование vclient

Команды ниже запускаются из корня проекта. Чтобы остановить работающий клиент,
нажмите Ctrl+C.

## Флаги CLI

Список сверён с регистрациями флагов в `cmd/vclient/main.go`.

| Флаг | Значение по умолчанию | Назначение |
| --- | --- | --- |
| `-config <path>` | пусто | Путь к JSON-профилю vclient |
| `-link <vless://...>` | пусто | Ссылка VLESS для быстрого старта |
| `-listen-socks <addr>` | `127.0.0.1:1080` | Адрес локального SOCKS inbound |
| `-listen-http <addr>` | `127.0.0.1:1081` | Адрес локального HTTP inbound |
| `-log-level <level>` | `info` | Уровень: `debug`, `info`, `warn`, `error` |
| `-log-file <path>` | пусто | Дополнительный файл журнала |
| `-rules <path>` | пусто | JSON-файл правил маршрутизации |
| `-dns <path>` | пусто | JSON-файл серверов DNS |
| `-sub-url <url>` | пусто | URL подписки VLESS |
| `-sub-interval <duration>` | `6h` | Период обновления подписки; Go duration, например `30m` или `6h` |
| `-profile <name>` | пусто | Имя профиля для выбора из подписки; без него используется первый профиль |
| `-reload-token <token>` | пусто | Токен локального Windows reload endpoint |
| `-system-proxy` | `false` | На Windows включает системный прокси пользователя; на других ОС только предупреждает |
| `-allow-experimental` | `false` | Разрешает экспериментальный mKCP |
| `-version` | `false` | Выводит версию приложения и завершает работу |

`-config` и `-link` несовместимы. `-sub-url` несовместим с обоими; `-profile`
требует `-sub-url`. `-sub-interval` должен быть положительным. Указание
`-sub-interval` без подписки не включает периодическое обновление.

**Замечание о справке:** в текущем CLI `go run ./cmd/vclient -h` не печатает
перечень флагов: `FlagSet` пишет в `io.Discard`, а разбор возвращает ошибку
`flag: help requested`. Поэтому таблица выше сверена с кодом регистрации
флагов; это поведение `-h` не исправлено в рамках этапа документации.

## Запуск с VLESS-ссылкой

В первом PowerShell окне:

```powershell
go run ./cmd/vclient -link "vless://UUID@server.example.com:443?type=tcp&security=tls&sni=server.example.com#primary"
```

Во втором PowerShell окне:

```powershell
curl.exe --socks5 127.0.0.1:1080 https://ifconfig.me
```

В Linux используйте `go run` в одном терминале, а `curl --socks5
127.0.0.1:1080 https://ifconfig.me` — в другом. Подробнее о параметрах ссылки
и сочетаниях транспорта/защиты — в [документе о транспортах](./transports.md).

## Собственный `config.json`

`-config` принимает один профиль в упрощённом формате vclient, а не JSON
конфигурации Xray-core:

```powershell
go run ./cmd/vclient -config .\profile.json
```

Схему, типы и пример профиля см. в [формате профиля](./profile-format.md).
Файл не включает маршрутизацию и список серверов DNS: для них используются
отдельные параметры `-rules` и `-dns`.

## Подписки и кэш

```powershell
go run ./cmd/vclient -sub-url "https://example.invalid/subscription" -profile "home" -sub-interval 6h
```

Имя выбирается точно и чувствительно к регистру. Если `-profile` не указан,
используется первая запись. Подписка может быть текстовым списком `vless://` ссылок или Base64-кодированным
списком. Последний успешно разобранный список
хранится в `cache/subscription.json` относительно текущей рабочей директории.
При ошибке загрузки клиент пытается прочитать кэш; при отсутствии либо
повреждении кэша возвращается ошибка. Во время работы загрузка повторяется с
интервалом `-sub-interval`.

## Маршрутизация и DNS

Примеры `rules.json`, `dns.json`, поддерживаемых условий, действий и требований
к geodata приведены в [документе о маршрутизации и DNS](./routing-dns.md).

```powershell
go run ./cmd/vclient -config .\profile.json -rules .\rules.json -dns .\dns.json
```

Пользовательские routing-правила добавляются перед правилом по умолчанию,
которое направляет TCP/UDP в `proxy`. Встроенные теги outbound: `proxy`,
`direct` и `block`.

## Перезагрузка

На ОС с поддержкой `SIGHUP` отправьте процессу этот сигнал для повторного
чтения профиля и связанных настроек. На Windows задайте непустой секрет
`-reload-token` и отправьте `POST` на `http://127.0.0.1:9090/reload` с
заголовком `X-Reload-Token`. Подробности — в [управлении процессом](./process-management.md).

## Журнал

Журнал выводится в stdout. При задании `-log-file` записи также добавляются в
этот файл. Выберите уровень, например:

```powershell
go run ./cmd/vclient -link "vless://UUID@server.example.com:443?type=tcp&security=tls&sni=server.example.com" -log-level debug -log-file .\vclient.log
```

UUID не включается в стартовое сообщение приложения; Xray-core может выдавать
свои диагностические сообщения в соответствии со своей конфигурацией.
