# Маршрутизация и DNS

## Файл правил

Передайте `-rules rules.json`, чтобы добавить field-правила перед правилом
маршрутизации по умолчанию (`tcp,udp` в `proxy`). Пример:

```json
[
  {
    "type": "field",
    "domain": ["geosite:cn", "domain:example.cn"],
    "outboundTag": "direct"
  },
  {
    "type": "field",
    "ip": ["geoip:private", "192.168.0.0/16"],
    "port": "80,443,1000-2000",
    "network": ["tcp", "udp"],
    "action": "block"
  }
]
```

Вместо `outboundTag` можно задать `action`: `proxy`, `direct` или `block`.
Одновременно указывать оба поля можно только с одинаковым значением.
Условия `domain` поддерживают префиксы Xray `domain:`, `full:`, `keyword:`,
`regexp:` и `geosite:`. Условия `ip` принимают IP-адрес, CIDR или `geoip:`.
Порты задаются строкой со списком портов и диапазонов; сеть — `tcp` и/или
`udp`. Все целевые outbound-теги проверяются при загрузке; доступны `proxy`,
`direct` и `block`.

Если используются `geosite:` или `geoip:`, соответствующий файл данных должен
существовать: `geosite.dat` для `geosite:` и `geoip.dat` для `geoip:`.
Каталог можно задать переменной `XRAY_LOCATION_ASSET` (эквивалент Xray
`xray.location.asset`). Без переменной Xray ищет данные в каталоге исполняемого
файла и стандартных каталогах платформы. Отсутствующая база приводит к ошибке
до запуска ядра.

## DNS

Передайте `-dns dns.json`. Минимальная конфигурация:

```json
{
  "servers": [
    {
      "address": "1.1.1.1:53",
      "outboundTag": "direct",
      "domains": ["domain:internal.example"]
    },
    {
      "address": "https://dns.google/dns-query",
      "outboundTag": "proxy"
    }
  ]
}
```

Для обычного DNS указывается IP-адрес с портом, включая скобки для IPv6
(например, `[2001:db8::1]:53`). Для DoH поддерживается HTTPS URL. `domains`
задаёт, для каких доменов Xray выбирает DNS-сервер. `outboundTag` — тег
исходящего подключения самого DNS-запроса, поэтому он должен быть одним из
`proxy`, `direct` или `block`. Это позволяет отправлять DNS к различным
резолверам через proxy/direct и выбирать резолверы по доменным правилам.

Схемы routing field rule и DNS nameserver сверены с исходниками подключённого
Xray-core: `infra/conf/router.go` (`parseFieldRule`) и `infra/conf/dns.go`
(`NameServerConfig`).
