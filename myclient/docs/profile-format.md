# Формат профиля vclient

Файл конфигурации vclient содержит один JSON-объект `Profile`. Это упрощённый
формат приложения, а не JSON-конфигурация Xray-core. При запуске профиль
преобразуется в конфигурацию Xray-core.

## Поля верхнего уровня

| Поле | Тип | Описание |
| --- | --- | --- |
| `name` | строка | Имя профиля |
| `protocol` | строка | На этом этапе поддерживается только `vless` |
| `address` | строка | IP-адрес или имя сервера |
| `port` | число | Порт сервера от 1 до 65535 |
| `uuid` | строка | UUID пользователя |
| `encryption` | строка | Необязательное шифрование VLESS `mlkem768x25519plus`; `none` используется по умолчанию |
| `network` | строка | `tcp`, `ws`, `grpc`, `xhttp`, `httpupgrade`, `kcp` или `quic` |
| `security` | строка | `none`, `tls` или `reality` |

Все поля профиля:

| Поле | Тип | Обязательность и назначение |
| --- | --- | --- |
| `name` | строка | Обязательно; имя профиля |
| `protocol` | строка | Обязательно; только `vless` |
| `address` | строка | Обязательно; IP-адрес или hostname |
| `port` | число | Обязательно; от 1 до 65535 |
| `uuid` | строка | Обязательно; UUID пользователя |
| `flow` | строка | Необязательно; `xtls-rprx-vision` допускается только с TCP |
| `network` | строка | Обязательно; `tcp`, `ws`, `grpc`, `xhttp`, `httpupgrade`, `kcp` или `quic` |
| `security` | строка | Обязательно; `none`, `tls` или `reality` |
| `tls` | объект | Настройки TLS ниже |
| `reality` | объект | Настройки REALITY ниже |
| `ws` | объект | Параметры WebSocket |
| `grpc` | объект | Параметры gRPC |
| `xhttp` | объект | Параметры XHTTP |
| `http_upgrade` | объект | Параметры HTTPUpgrade |
| `kcp` | объект | Ограниченные параметры mKCP |

### Вложенные поля

| Объект | Поле | Тип | Значение |
| --- | --- | --- | --- |
| `tls` | `server_name` | строка | SNI; при отсутствии используется `address` |
| `tls` | `alpn` | массив строк | ALPN протоколов |
| `tls` | `fingerprint` | строка | TLS fingerprint, если задан |
| `tls` | `allow_insecure` | boolean | `true` сейчас не поддерживается Xray-core и приводит к ошибке |
| `reality` | `public_key` | строка | Обязательный для REALITY 43-символьный base64url-ключ |
| `reality` | `short_id` | строка | Hex, чётной длины, максимум 16 символов; пустое значение допустимо |
| `reality` | `spider_x` | строка | Необязательный SpiderX |
| `reality` | `server_name` | строка | SNI; при отсутствии используется `address` |
| `reality` | `fingerprint` | строка | Fingerprint; по умолчанию `chrome` |
| `reality` | `mldsa65_verify` | строка | Необязательный ML-DSA-65 verification key, base64url, декодированный размер 1952 байта; в ссылке VLESS передаётся как `pqv` |
| `ws` | `path` | строка | Обязательно, начинается с `/` |
| `ws` | `host` | строка | Необязательный Host |
| `grpc` | `service_name` | строка | Обязательно, не содержит пробелов |
| `grpc` | `multi_mode` | boolean | Включение gRPC multi-mode |
| `xhttp` | `path` | строка | Необязательный путь |
| `xhttp` | `host` | строка | Необязательный Host |
| `xhttp` | `mode` | строка | `auto`, `packet-up`, `stream-up` или `stream-one`; по умолчанию `auto` |
| `xhttp` | `headers` | объект строк | Дополнительные заголовки; ключи не пустые и не `Host` |
| `xhttp` | `x_padding_bytes` | строка | Необязательный положительный размер или диапазон, например `100-1000`; в Xray передаётся как `xPaddingBytes` |
| `http_upgrade` | `path` | строка | Обязательно, начинается с `/` |
| `http_upgrade` | `host` | строка | Необязательный Host |
| `kcp` | `mtu` | число | Необязательно; минимум 21 |
| `kcp` | `tti` | число | Необязательно; диапазон 10–1000 |
| `kcp` | `header_type` | строка | Не поддерживается, кроме пустого значения или `none` |
| `kcp` | `seed` | строка | Не поддерживается; непустое значение отклоняется |

Для `security: "reality"` обязательно указать `reality.public_key` — 43-символьный
base64url-ключ длиной 32 байта. `reality.short_id` должен быть hex-строкой
чётной длины не более 16 символов. Пустой short ID допустим.
Необязательные настройки транспорта и защиты задаются вложенными объектами
`tls`, `reality`, `ws`, `grpc`, `xhttp` и `http_upgrade`. Их поля соответствуют
экспортируемым полям Go-структуры `Profile` и используют имена в `snake_case`.
Для поддерживаемых сочетаний транспорта и безопасности см. [матрицу
транспортов](./transports.md). Flow `xtls-rprx-vision` разрешён только с `tcp`.
Для mKCP требуется CLI-флаг `-allow-experimental`; доступны только `kcp.mtu`
и `kcp.tti`. KCP header и seed, а также QUIC, не поддерживаются этой версией
Xray-core и отклоняются.
Для `grpc` обязателен `grpc.service_name` без пробелов; `grpc.multi_mode`
управляет режимом gRPC. Для `xhttp` можно задать `xhttp.path`, `xhttp.host`,
`xhttp.mode` и `xhttp.headers`. Допустимы режимы `auto`, `packet-up`, `stream-up`
и `stream-one`; пустое значение означает `auto`. Имена заголовков не должны
быть пустыми. Для `ws` обязательный путь задаётся в `ws.path`, а необязательный
Host — в `ws.host`. Для `httpupgrade` путь задаётся в `http_upgrade.path`, Host
— в `http_upgrade.host`; пути должны начинаться с `/`.
В текущей версии Xray-core `tls.allow_insecure: true` удалён и приводит к ошибке;
проверка сертификата не отключается.

Пример профиля TLS с WebSocket:

```json
{
  "name": "Пример",
  "protocol": "vless",
  "address": "proxy.example.com",
  "port": 443,
  "uuid": "123e4567-e89b-12d3-a456-426614174000",
  "network": "ws",
  "security": "tls",
  "tls": {
    "server_name": "proxy.example.com",
    "alpn": ["h2", "http/1.1"],
    "fingerprint": "chrome",
    "allow_insecure": false
  },
  "ws": {
    "path": "/socket",
    "host": "cdn.example.com"
  }
}
```
