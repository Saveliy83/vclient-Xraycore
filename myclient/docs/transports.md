# Поддержка транспортов

В таблице `да` означает, что vclient генерирует конфигурацию Xray для сочетания.
Фактическое соединение дополнительно требует соответствующей конфигурации
сервера. `Vision` — flow `xtls-rprx-vision`; он разрешён только для TCP.

| Транспорт | Безопасность | Flow | Дополнительные параметры |
| --- | --- | --- | --- |
| TCP | none | пустой или Vision | — |
| TCP | TLS | пустой или Vision | `tls.server_name`, `tls.alpn`, `tls.fingerprint` |
| TCP | REALITY | пустой или Vision | `reality.server_name`, `public_key`, `short_id`, `spider_x`, `fingerprint` |
| WebSocket (`ws`) | none или TLS | пустой | `ws.path` (обязателен, начинается с `/`), `ws.host` |
| HTTPUpgrade | none или TLS | пустой | `http_upgrade.path` (обязателен, начинается с `/`), `http_upgrade.host` |
| gRPC | TLS или REALITY | пустой | `grpc.service_name` обязателен и не содержит пробелов; `grpc.multi_mode` |
| XHTTP | TLS или REALITY | пустой | `xhttp.path`, `xhttp.host`, `xhttp.mode`, `xhttp.headers` |
| mKCP (`kcp`) | none или TLS | пустой | Экспериментально; `kcp.mtu`, `kcp.tti`; требуется `-allow-experimental` |
| QUIC | — | — | Не поддерживается связанной версией Xray-core |

Для gRPC и XHTTP используются `tlsSettings` или `realitySettings` согласно
значению `security`. Публичный ключ REALITY и Short ID проверяются при
валидации профиля. Поля XHTTP `mode`, `path`, `host` и `headers` соответствуют
схеме Xray-core; допустимые значения mode: `auto`, `packet-up`, `stream-up`,
`stream-one`. Если mode не задан, vclient генерирует `auto`. Пустые имена
заголовков не допускаются. Xray-core также запрещает `Host` в `xhttp.headers`;
для имени хоста используйте отдельное поле `xhttp.host`.

Flow в таблице описывает ограничение профиля vclient: `xtls-rprx-vision`
допускается только с TCP. Безопасность `none` поддерживается генератором для
TCP и WebSocket/HTTPUpgrade; flow следует использовать с конфигурацией сервера,
поддерживающей Vision.

REALITY поддерживается для TCP, gRPC и XHTTP. Подробности получения публичного
ключа и Short ID приведены в [инструкции REALITY](./reality.md).

mKCP зарегистрирован в связанной версии Xray-core. Однако `infra/conf.KCPConfig`
в этой версии переносит в runtime-конфигурацию только `mtu` и `tti`; параметры
`header` и `seed` не используются, поэтому vclient отклоняет их вместо того,
чтобы молча принимать неработающие настройки. По той же причине QUIC не
генерируется: конвертер transport name в
`infra/conf/transport_internet.go` помечает старый QUIC транспорт удалённым.
