# Rabbit Hole TURN Proxy

> [!CAUTION]
> **Назначение проекта — исследование, тестирование и администрирование разрешённой инфраструктуры.** Используйте проект только с сетями, системами, учётными записями и трафиком, которые принадлежат вам либо на работу с которыми владелец заранее дал явное разрешение.

## Правомерное использование, ограничения и ответственность

Проект представляет собой исходный код общего назначения и не предназначен для неправомерного доступа к компьютерной информации, вмешательства в работу чужих систем, перехвата или изменения чужого трафика, использования чужих учётных данных либо получения доступа к ресурсам без законного основания. Не используйте проект для распространения запрещённой информации или оказания услуг третьим лицам без необходимых прав, разрешений и соблюдения обязательных требований.

До использования, изменения или распространения проекта пользователь обязан самостоятельно проверить законность конкретного сценария, наличие необходимых полномочий, а также соблюдение применимого законодательства, прав владельцев инфраструктуры и условий сторонних платформ. Если законность или объём разрешения неочевидны, использование следует прекратить до получения индивидуальной юридической консультации.

В пределах, допускаемых применимым законодательством, программное обеспечение предоставляется «как есть», без гарантий пригодности для конкретной цели, бесперебойной работы или сохранности данных. Никакое положение этого раздела не исключает и не ограничивает ответственность в случаях, когда такое исключение или ограничение запрещено законом.

Работа сторонних TURN-сервисов, API и сетей не гарантируется: их протоколы, ограничения и доступность могут измениться без предупреждения.

Rabbit Hole TURN Proxy — серверный форк VK TURN Proxy для передачи локального UDP/TCP-трафика через TURN-реле, получаемых из ссылки на VK Calls. Существующие клиенты iPhone продолжают работать по исходному протоколу.

Проект основан на [Moroka8/vk-turn-proxy](https://github.com/Moroka8/vk-turn-proxy) и сохраняет совместимость с [оригинальным vk-turn-proxy](https://github.com/cacggghp/vk-turn-proxy).

Android-клиент: [Haeniken/rabbithole-turn-android](https://github.com/Haeniken/rabbithole-turn-android).

Исходный Android-клиент: [kiper292/wireguard-turn-android](https://github.com/kiper292/wireguard-turn-android).

iPhone-клиент: [TestFlight](https://testflight.apple.com/join/ANm6cmDv); [исходный код](https://github.com/anton48/vk-turn-proxy-ios).

## Содержание

- [Как это работает](#как-это-работает)
- [Совместимость и агрегация UDP-сессий](#совместимость-и-агрегация-udp-сессий)
- [Возможности](#возможности)
- [Что нужно](#что-нужно)
- [Быстрый старт: WireGuard](#быстрый-старт-wireguard)
  - [Запуск сервера на VPS](#1-запустите-сервер-на-vps)
  - [Настройка WireGuard](#2-настройте-wireguard-на-клиенте)
  - [Запуск клиента](#3-запустите-клиент)
- [Android через Termux](#android-через-termux)
- [iOS через iSH](#ios-через-ish)
- [systemd-сервис](#сервер-как-systemd-сервис)
- [Docker](#docker)
- [VLESS / Xray](#vless--xray)
- [WRAP-режим](#wrap-режим)
- [Яндекс Телемост](#яндекс-телемост)
- [Флаги клиента](#флаги-клиента)
- [Флаги сервера](#флаги-сервера)
- [Captcha](#captcha)
- [Сборка из исходников](#сборка-из-исходников)
- [Решение проблем](#решение-проблем)
- [Похожие проекты](#похожие-проекты)
- [Лицензия](#лицензия)

## Как это работает

Схема для WireGuard:

```text
WireGuard client -> 127.0.0.1:9000 -> Rabbit Hole TURN Proxy client
  -> VK TURN relay -> Rabbit Hole TURN Proxy server на VPS
  -> 127.0.0.1:<порт WireGuard> -> WireGuard server
```

Клиент берет временные TURN-учетные данные из ссылки VK Calls, открывает одно или несколько соединений к TURN-реле и отправляет через них трафик к вашему `server`. Между `client` и `server` используется DTLS. Для WireGuard сервер пересылает данные в UDP backend, для VLESS/Xray — в TCP backend через KCP и smux.

## Совместимость и агрегация UDP-сессий

В режиме `proxy_v2` Android отправляет перед обычными пакетами 17-байтовый префикс: UUID сессии и номер TURN-потока. Сервер объединяет такие потоки в один UDP socket к WireGuard backend. Это устраняет постоянное roaming-переключение endpoint WireGuard между разными backend-сокетами.

- Новый Android-клиент выставляет capability-флаг поддержки bounded reorder-буфера. Сервер выдаёт нисходящий трафик короткими стабильными полосами и меняет линию на границе flowlet либо при очереди на текущей линии.
- Старый Android-клиент с тем же 17-байтовым префиксом также получает единый backend socket, но нисходящий трафик закрепляется за одной линией и не полосуется.
- iPhone и `proxy_v1` не отправляют префикс и обслуживаются прежним способом: одна DTLS-сессия — один UDP socket к backend.

Новый формат не меняет DTLS, WRAP, WireGuard-пакеты или получение TURN credentials. Обновлять iPhone-клиент не требуется.

## Возможности

- VK Calls как основной источник TURN-учетных данных.
- TCP или UDP подключение клиента к TURN-реле.
- Несколько параллельных TURN-потоков через `-n`.
- WireGuard/Hysteria-подобный UDP backend.
- VLESS/Xray TCP backend через `-vless`.
- Bonding для VLESS через `-vless-bond`.
- Дополнительная WRAP-обфускация DTLS-пакетов через `-wrap`.
- Агрегация Android TURN-потоков в одну UDP-сессию WireGuard без изменения протокола iPhone.
- Flowlet/stripe-планирование нисходящего трафика для клиентов с reorder-буфером.
- Graceful drain установленных сессий при `SIGTERM`.
- Prometheus-метрики, readiness/health endpoints и мягкий admission control.
- Изолированный canary-режим с отдельным лимитом новых сессий.
- Автоматическое и ручное прохождение VK captcha.
- Docker-образ для серверной части.

## Что нужно

- VPS с публичным IP.
- На VPS уже должен слушать backend:
  - WireGuard: обычно `127.0.0.1:51820/udp`;
  - Xray/VLESS: обычно `127.0.0.1:443/tcp`.
- Ссылка на активный VK Calls вида `https://vk.com/call/join/...`.
- На клиенте: WireGuard, Xray или другой локальный клиент, который будет ходить в `127.0.0.1:9000`.

Ссылку VK Calls лучше создать самостоятельно. Не завершайте звонок для всех, если хотите использовать эту ссылку дальше.

## Быстрый старт: WireGuard

### 1. Запустите сервер на VPS

Соберите актуальный сервер для Linux amd64:

```bash
git clone --branch v2.1 https://github.com/Haeniken/rabbithole-turn-proxy.git
cd rabbithole-turn-proxy
CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o server ./server
```

Запустите `server`, указав локальный адрес WireGuard:

```bash
./server -listen 0.0.0.0:56000 -connect 127.0.0.1:51820
```

Порт `56000/udp` должен быть доступен снаружи. Если WireGuard слушает другой порт, замените `51820`.

### 2. Настройте WireGuard на клиенте

В клиентском конфиге WireGuard замените endpoint сервера на локальный адрес VK TURN Proxy:

```ini
Endpoint = 127.0.0.1:9000
MTU = 1280
```

На Android добавьте Termux или приложение-клиент в исключения WireGuard. Нативный Android-клиент автоматически использует MTU 1264; значение выше приведено для универсального CLI-примера. На Windows, Linux и macOS перед включением WireGuard нужно добавить маршрут до TURN-реле, иначе клиент может попытаться подключаться к TURN уже через сам туннель.

### 3. Запустите клиент

Linux:

```bash
curl -L -o client https://github.com/cacggghp/vk-turn-proxy/releases/latest/download/client-linux-amd64
chmod +x client
./client -listen 127.0.0.1:9000 -peer <ip-vps>:56000 -vk-link "<vk-call-link>" | ./routes.sh
```

Windows PowerShell от администратора:

```powershell
Invoke-WebRequest -Uri https://github.com/cacggghp/vk-turn-proxy/releases/latest/download/client-windows-amd64.exe -OutFile client.exe
.\client.exe -listen 127.0.0.1:9000 -peer <ip-vps>:56000 -vk-link "<vk-call-link>" | .\routes.ps1
```

macOS:

```bash
curl -L -o client https://github.com/cacggghp/vk-turn-proxy/releases/latest/download/client-darwin-arm64
chmod +x client
./client -listen 127.0.0.1:9000 -peer <ip-vps>:56000 -vk-link "<vk-call-link>" | ./routes-macos.sh
```

После появления соединения включите WireGuard.

Если вы скачали только бинарник, но не клонировали репозиторий, возьмите нужный route-скрипт из этого репозитория: `routes.sh`, `routes.ps1` или `routes-macos.sh`.

## Android через Termux

1. Установите Termux из F-Droid.
2. В WireGuard укажите `Endpoint = 127.0.0.1:9000` и `MTU = 1280`.
3. Добавьте Termux в исключения WireGuard.
4. Запустите в Termux:

```bash
termux-wake-lock
curl -L -o client https://github.com/cacggghp/vk-turn-proxy/releases/latest/download/client-android-arm64
chmod +x client
./client -listen 127.0.0.1:9000 -peer <ip-vps>:56000 -vk-link "<vk-call-link>"
```

Чтобы снять wake lock:

```bash
termux-wake-unlock
```

## iOS через iSH

Это запасной вариант, если нет нативного клиента.

```bash
apk update
apk add curl
curl -L -o client https://github.com/cacggghp/vk-turn-proxy/releases/latest/download/client-linux-386
chmod +x client
GOMAXPROCS=1 GODEBUG=asyncpreemptoff=1 ./client -listen 127.0.0.1:9000 -peer <ip-vps>:56000 -vk-link "<vk-call-link>"
```

Чтобы iSH дольше жил в фоне, можно в начале сессии выполнить:

```bash
cat /dev/location > /dev/null &
```

## Сервер как systemd-сервис

Пример `/etc/systemd/system/rabbithole-turn-proxy.service`:

```ini
[Unit]
Description=Rabbit Hole TURN Proxy server
After=network.target

[Service]
Type=simple
ExecStart=/opt/rabbithole-turn-proxy/server -listen 0.0.0.0:56000 -connect 127.0.0.1:51820
Restart=always
RestartSec=5
User=nobody
Group=nogroup

[Install]
WantedBy=multi-user.target
```

Применить:

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now rabbithole-turn-proxy.service
sudo systemctl status rabbithole-turn-proxy.service
```

## Docker

Соберите образ из этого репозитория:

```bash
docker build -t rabbithole-turn-proxy .
```

Для Compose скопируйте безопасный шаблон и задайте локальные значения в
неотслеживаемом `.env`:

```bash
cp docker-compose.yml.example docker-compose.yml
cp .env.example .env
docker compose config -q
docker compose up -d rabbithole-turn-proxy
```

В `.env.example` перечислены все параметры Compose с безопасными значениями и
русскими комментариями. Рабочий `.env` исключён из Git; секретный `WRAP_KEY`
нельзя добавлять в коммиты, логи или снимки экрана.

Canary из того же файла запускается отдельно и слушает другой порт:

```bash
docker compose --profile canary up -d rabbithole-turn-proxy-canary
```

Если используется WRAP, задайте в `.env` `WRAP_MODE=true` и `WRAP_KEY`, не
добавляя сам `.env` в репозиторий.

Если backend слушает на хосте, удобнее использовать host network:

```bash
docker run --rm --network host \
  -e CONNECT_ADDR=127.0.0.1:51820 \
  -e DRAIN_TIMEOUT=30s \
  rabbithole-turn-proxy
```

Переменные окружения:

| Переменная | По умолчанию | Описание |
| --- | --- | --- |
| `CONNECT_ADDR` | обязательна | backend, куда сервер пересылает трафик |
| `LISTEN_ADDR` | `0.0.0.0:56000` | адрес прослушивания сервера |
| `VLESS_MODE` | `false` | включает `-vless` |
| `VLESS_BOND` | `false` | включает `-vless-bond` |
| `WRAP_MODE` | `false` | включает `-wrap` |
| `WRAP_KEY` | пусто | ключ для `-wrap-key` |
| `DRAIN_TIMEOUT` | `30s` | сколько ждать завершения активных сессий после `SIGTERM` |
| `METRICS_LISTEN` | `127.0.0.1:9090` | адрес HTTP endpoints `/healthz`, `/readyz`, `/metrics`; пустое значение отключает listener |
| `INSTANCE_NAME` | `default` | метка экземпляра в метриках |
| `MAX_SESSIONS` | `2048` | мягкий предел одновременно принятых DTLS-сессий; `0` отключает проверку |
| `MAX_HANDSHAKES` | `128` | предел одновременно выполняемых DTLS handshakes |
| `MAX_GOROUTINES` | `20000` | отказ новым сессиям при достижении числа goroutine |
| `MIN_FREE_FDS` | `128` | резерв файловых дескрипторов, недоступный новым сессиям |
| `CANARY_MODE` | `false` | помечает отдельный listener как canary |
| `CANARY_MAX_SESSIONS` | `64` | дополнительный предел сессий для canary |
| `VK_TURN_KCP_PROFILE` | `balanced` | профиль KCP (`fast`, `balanced`, `slow`) |
| `VK_TURN_KCP_MTU` | `1200` | переопределить MTU для KCP |

Bridge mode:

```bash
docker run --rm -p 56000:56000/udp \
  -e CONNECT_ADDR=<host-ip>:51820 \
  -e DRAIN_TIMEOUT=30s \
  rabbithole-turn-proxy
```

## Наблюдаемость и защита от перегрузки

HTTP listener по умолчанию доступен только локально на
`127.0.0.1:9090`:

```bash
curl -fsS http://127.0.0.1:9090/healthz
curl -fsS http://127.0.0.1:9090/readyz
curl -fsS http://127.0.0.1:9090/metrics
```

`/healthz` показывает, что процесс жив. `/readyz` возвращает `503`, когда
сервер находится в drain или временно не принимает новые сессии из-за
лимита сессий, handshakes, goroutine либо файловых дескрипторов. Существующие
соединения при этом продолжают работать.

Метрики включают число активных и установленных сессий, handshakes,
goroutine, открытых FD, размеры агрегированных очередей, потери при заполнении
очередей, backend-ошибки и результат graceful drain. Listener не следует
публиковать в Интернет без отдельной аутентификации или сетевого ACL.

## Canary-деплой

Canary запускается отдельным процессом или контейнером на другом UDP-порту.
Нельзя включать случайный canary внутри основного listener: после принятия
UDP/DTLS-сессии её невозможно безопасно передать стабильному процессу.

Пример отдельного экземпляра:

```bash
docker run --rm --network host \
  -e CONNECT_ADDR=127.0.0.1:51820 \
  -e LISTEN_ADDR=0.0.0.0:56001 \
  -e METRICS_LISTEN=127.0.0.1:9091 \
  -e INSTANCE_NAME=canary \
  -e CANARY_MODE=true \
  -e CANARY_MAX_SESSIONS=32 \
  rabbithole-turn-proxy
```

На canary-порт направляют только заранее выбранные тестовые конфигурации.
После проверки `/readyz`, ошибок backend, очередей, потерь и ресурсов тот же
образ последовательно применяется к стабильным экземплярам с graceful drain.

## VLESS / Xray

В режиме `-vless` Rabbit Hole TURN Proxy прокидывает TCP-соединения. На VPS `server` подключается к локальному TCP backend, например к Xray inbound на `127.0.0.1:443`. На клиенте `client` слушает локальный TCP адрес, на который должен смотреть ваш Xray/v2rayN/sing-box клиент.

Сервер:

```bash
./server -listen 0.0.0.0:56000 -connect 127.0.0.1:443 -vless
```

Клиент:

```bash
./client -listen 127.0.0.1:9000 -peer <ip-vps>:56000 -vk-link "<vk-call-link>" -vless
```

С bonding:

```bash
./server -listen 0.0.0.0:56000 -connect 127.0.0.1:443 -vless -vless-bond
./client -listen 127.0.0.1:9000 -peer <ip-vps>:56000 -vk-link "<vk-call-link>" -vless -vless-bond -n 4
```

## WRAP-Режим

`-wrap` дополнительно оборачивает DTLS-пакеты в SRTP-подобный контейнер с ChaCha20-Poly1305 AEAD перед отправкой в TURN ChannelData. Ключ должен совпадать на клиенте и сервере.

Сгенерировать ключ:

```bash
./server -gen-wrap-key
```

Запуск:

```bash
./server -listen 0.0.0.0:56000 -connect 127.0.0.1:51820 -wrap -wrap-key <64-hex-key>
./client -listen 127.0.0.1:9000 -peer <ip-vps>:56000 -vk-link "<vk-call-link>" -wrap -wrap-key <64-hex-key>
```

`-wrap` нельзя использовать вместе с `-no-dtls`.

## Настройка KCP (VLESS)

В режиме `-vless` для передачи данных поверх DTLS используется KCP. Его можно настроить через переменные окружения (работает и для клиента, и для сервера):

| Переменная | Профили / Значения | Описание |
| --- | --- | --- |
| `VK_TURN_KCP_PROFILE` | `fast`, `balanced`, `slow` | Предустановленные режимы работы KCP. |
| `VK_TURN_KCP_MTU` | например, `1200` | Максимальный размер пакета. |

**Профили:**
- `fast` (или `legacy`): Минимальные задержки, активная переотправка, MTU 1280.
- `balanced` (или `cc`): Оптимальный баланс для большинства сетей, MTU 1200.
- `slow` (или `conservative`): Для очень нестабильных каналов, MTU 1150.

Для более тонкой настройки доступны переменные: `VK_TURN_KCP_NODELAY`, `VK_TURN_KCP_INTERVAL`, `VK_TURN_KCP_RESEND`, `VK_TURN_KCP_NC`, `VK_TURN_KCP_SNDWND`, `VK_TURN_KCP_RCVWND`, `VK_TURN_KCP_ACK_NODELAY`.


## Яндекс Телемост

Поддержка `-yandex-link` оставлена в коде, но этот режим считается нестабильным и может не работать. Если используете его, обычно нужен `-udp` и ручной TURN IP:

```bash
./client -udp -turn 5.255.211.241 -listen 127.0.0.1:9000 -peer <ip-vps>:56000 -yandex-link "<telemost-link>"
```

## Флаги клиента

| Флаг | По умолчанию | Описание |
| --- | --- | --- |
| `-listen` | `127.0.0.1:9000` | локальный адрес для WireGuard или Xray клиента |
| `-peer` | обязательный | адрес Rabbit Hole TURN Proxy server на VPS, например `<ip-vps>:56000` |
| `-vk-link` | пусто | ссылка VK Calls |
| `-yandex-link` | пусто | ссылка Яндекс Телемоста, legacy-режим |
| `-n` | VK: `10`, Yandex: `1` | количество TURN-соединений |
| `-udp` | `false` | подключаться к TURN-реле по UDP вместо TCP |
| `-turn` | из ссылки | переопределить IP TURN-сервера |
| `-port` | из ссылки | переопределить порт TURN-сервера |
| `-vless` | `false` | TCP/VLESS режим |
| `-vless-bond` | `false` | распределять одно TCP-соединение по активным smux-сессиям |
| `-wrap` | `false` | включить WRAP-обфускацию |
| `-wrap-key` | пусто | 32-байтный ключ в hex, 64 символа |
| `-gen-wrap-key` | `false` | напечатать новый WRAP-ключ и выйти |
| `-manual-captcha` | `false` | сразу использовать ручное прохождение captcha |
| `-captcha-host` | пусто | host:port для manual captcha, например `192.168.99.1:8765` |
| `-captcha-solver` | `v2` | авто-решатель captcha: `v1` или `v2` |
| `-streams-per-cred` | `10` | сколько потоков используют один кеш TURN-учетных данных |
| `-debug` | `false` | подробные логи |
| `-no-dtls` | `false` | прямой режим без DTLS, не рекомендуется |

Нужно указать ровно одну ссылку: `-vk-link` или `-yandex-link`.

## Флаги сервера

| Флаг | По умолчанию | Описание |
| --- | --- | --- |
| `-listen` | `0.0.0.0:56000` | адрес прослушивания |
| `-connect` | обязательный | backend-адрес, например `127.0.0.1:51820` или `127.0.0.1:443` |
| `-vless` | `false` | TCP/VLESS режим |
| `-vless-bond` | `false` | bonding для VLESS |
| `-wrap` | `false` | включить WRAP-обфускацию |
| `-wrap-key` | пусто | 32-байтный ключ в hex, 64 символа |
| `-gen-wrap-key` | `false` | напечатать новый WRAP-ключ и выйти |
| `-drain-timeout` | `30s` | максимальное ожидание завершения активных сессий после `SIGTERM`; `0` завершает сразу |
| `-metrics-listen` | `127.0.0.1:9090` | HTTP endpoints наблюдаемости; пустое значение отключает listener |
| `-instance-name` | `default` | метка экземпляра в метриках |
| `-max-sessions` | `2048` | мягкий предел активных DTLS-сессий |
| `-max-handshakes` | `128` | предел одновременных DTLS handshakes |
| `-max-goroutines` | `20000` | предел goroutine для допуска новых сессий |
| `-min-free-fds` | `128` | резерв свободных файловых дескрипторов |
| `-canary` | `false` | пометить отдельный listener как canary |
| `-canary-max-sessions` | `64` | дополнительный предел сессий canary |
| `-debug` | `false` | подробные логи |

## Captcha

Для VK Calls клиент умеет автоматически проходить captcha. Если автоматика не сработала, включается ручной сценарий через локальный браузер. Можно сразу запросить ручной режим:

```bash
./client -manual-captcha -listen 127.0.0.1:9000 -peer <ip-vps>:56000 -vk-link "<vk-call-link>"
```

Профиль браузера сохраняется в `vk_profile.json` рядом с бинарником и может помочь последующим запросам выглядеть последовательнее.

## Сборка из исходников

Нужен Go 1.25.x.

```bash
go build -o client ./client
go build -o server ./server
go test ./...
```

Кросс-сборка примера для Linux amd64:

```bash
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o server-linux-amd64 ./server
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o client-linux-amd64 ./client
```

## Решение проблем

- Сначала запускайте Rabbit Hole TURN Proxy client, потом включайте WireGuard.
- Если WireGuard забирает весь трафик, добавьте маршрут до IP TURN-реле через `routes.sh`, `routes.ps1` или `routes-macos.sh`.
- Если TCP до TURN не работает, попробуйте `-udp`.
- Если соединение нестабильное, попробуйте уменьшить `-n`, например `-n 1`.
- Если VK просит captcha слишком часто, попробуйте `-manual-captcha`, затем повторите обычный запуск.
- Если клиент зависает на получении TURN-данных, проверьте, что ссылка VK Calls живая и не была завершена для всех.
- Если сервер запущен в Docker bridge mode, `CONNECT_ADDR=127.0.0.1:51820` укажет внутрь контейнера, а не на хост. Используйте host network или IP хоста.
- Если включен `-wrap`, убедитесь, что и клиент, и сервер используют одинаковый `-wrap-key`.

## Похожие Проекты

Авторы этого репозитория не отвечают за работу сторонних проектов.

Server:

- https://github.com/Urtyom-Alyanov/turn-proxy - реализация на Rust.
- https://github.com/jaykaiperson/lionheart - похожий подход для `stream.wb.ru`.
- https://github.com/kulikov0/whitelist-bypass - проброс через медиасерверы.
- https://github.com/NedgNDG/vk-proxy-auto-installer - автоустановщик VK TURN Proxy.
- https://github.com/defin85/vk-turn-proxy-go

Android:

- https://github.com/samosvalishe/turn-proxy-android
- https://github.com/MYSOREZ/vk-turn-proxy-android
- https://github.com/kiper292/wireguard-turn-android
- https://github.com/WINGS-N/WINGSV
- https://github.com/oxsidee/vkpn
- https://github.com/amurcanov/proxy-turn-vk-android

iOS:

- https://github.com/nullcstring/turnbridge
- https://github.com/iamdiviem/turnbridge
- https://github.com/anton48/vk-turn-proxy-ios

macOS:

- https://github.com/denny4-user/vk-turn-proxy-macos-gui

## Лицензия

GPL-3.0. См. [LICENSE](LICENSE).

<a href="https://www.star-history.com/?repos=cacggghp%2Fvk-turn-proxy&type=date&legend=top-left">
 <picture>
   <source media="(prefers-color-scheme: dark)" srcset="https://api.star-history.com/chart?repos=cacggghp/vk-turn-proxy&type=date&theme=dark&legend=top-left" />
   <source media="(prefers-color-scheme: light)" srcset="https://api.star-history.com/chart?repos=cacggghp/vk-turn-proxy&type=date&legend=top-left" />
   <img alt="Star History Chart" src="https://api.star-history.com/chart?repos=cacggghp/vk-turn-proxy&type=date&legend=top-left" />
 </picture>
</a>
