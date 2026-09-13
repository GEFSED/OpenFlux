<div align="center">
  <img src="design/logo/avatar.svg" width="112" alt="Логотип OpenFlux">
  <h1>OpenFlux Android</h1>
  <p>Зашифрованный VPN через документ-транспорт для Android, компьютера и выходной Linux-ноды.</p>
  <p>
    <a href="https://github.com/damnurmum/OpenFlux-Android/releases/latest"><img src="https://img.shields.io/github/v/release/damnurmum/OpenFlux-Android?display_name=tag&amp;sort=semver&amp;style=flat-square&amp;color=7aa2f7" alt="Последний релиз"></a>
    <a href="https://github.com/damnurmum/OpenFlux-Android/actions/workflows/ci.yml"><img src="https://github.com/damnurmum/OpenFlux-Android/actions/workflows/ci.yml/badge.svg" alt="Статус CI"></a>
    <a href="LICENSE"><img src="https://img.shields.io/github/license/damnurmum/OpenFlux-Android?style=flat-square" alt="Лицензия GPL-3.0"></a>
    <img src="https://img.shields.io/badge/Android-8.0%2B-3DDC84?style=flat-square&amp;logo=android&amp;logoColor=white" alt="Android 8 или новее">
  </p>
  <p>
    <img src="https://img.shields.io/badge/Go-1.26.4%2B-00ADD8?style=flat-square&amp;logo=go&amp;logoColor=white" alt="Go 1.26.4 или новее">
    <img src="https://img.shields.io/badge/Java-17-ED8B00?style=flat-square&amp;logo=openjdk&amp;logoColor=white" alt="Java 17">
    <img src="https://img.shields.io/badge/ABI-ARM64%20%7C%20ARMv7%20%7C%20x86__64%20%7C%20x86-455a64?style=flat-square" alt="Поддерживаемые архитектуры Android">
    <img src="https://img.shields.io/badge/IPv4%20%2F%20TCP-experimental-f59e0b?style=flat-square" alt="Экспериментальная поддержка IPv4 и TCP">
  </p>
  <p><a href="README.md">English</a> · <strong>Русский</strong></p>
</div>

![OpenFlux Android: подключение, логи и настройки](docs/images/openflux-android-tabs.png)

> Это экспериментальный независимо развиваемый форк
> [p1neappleXpress/OpenFlux](https://github.com/p1neappleXpress/OpenFlux).
> Основные отличия от исходного проекта перечислены в [FORK.md](docs/FORK.md).
> `main` совместим по протоколу с текущими бинарниками exit node/клиента
> апстрима; отдельная ветка `experimental` несёт дополнительные фичи
> (график пинга, страна exit node, DNS-резолвинг на сервере), которым нужен
> exit node именно этого форка - см. [docs/UPSTREAM_DIFF.md](docs/UPSTREAM_DIFF.md).

OpenFlux - исследовательский TCP-туннель с подключаемыми транспортами. В этом
форке добавлены Android VPN-клиент и опциональное сквозное шифрование для
транспорта через Yandex Docs.

**[Скачать последнюю Android-версию](https://github.com/damnurmum/OpenFlux-Android/releases/latest)**

Впервые здесь? [docs/GUIDE.ru.md](docs/GUIDE.ru.md) - подробный пошаговый
гайд для новичков: как развернуть exit-node на VPS и подключиться с Android.

```text
Android VPN или SOCKS5-клиент -> зашифрованный транспорт -> Linux-нода -> интернет
```

## Возможности

- Android-клиент для Android 8+ на системном `VpnService` со сборками для ARM,
  ARM64, x86 и x86_64;
- второй режим подключения на Android - локальный SOCKS5-прокси, если не
  нужен полноценный системный VPN, с опциональным доступом из локальной сети,
  авторизацией по логину/паролю и ссылкой-QR для подключения с другого
  устройства;
- интерфейс в стиле Android 11 с подключением, логами и вертикальной
  навигацией по настройкам в стиле системных настроек телефона;
- опциональное аутентифицированное шифрование AES-256-GCM с получением ключа
  через scrypt, совместимое по протоколу с бинарниками exit node/клиента
  апстрима; можно оставить ключ пустым и подключаться без шифрования к
  обычной ноде апстрима;
- хранение ссылки и общего секрета с защитой Android Keystore;
- поле DNS-сервера принимает любой IP или доменное имя (не фиксированный
  список провайдеров), резолвится локально на устройстве;
- маршрутизация по приложениям (белый или чёрный список для туннеля);
- закреплённое уведомление с живой скоростью отдачи/приёма и кнопкой
  отключения, для обоих режимов;
- SOCKS5-клиент для компьютера и режим выходной Linux-ноды;
- транспорт через Yandex Docs и экспериментальный транспорт через MAX.

> **Предупреждение о MAX-транспорте:** транспорт MAX отправляет пакеты через
> WebRTC DataChannel с использованием вашего MAX-аккаунта. Не используйте
> основной или важный аккаунт; запуск через внешний VPS может привести к
> ограничению аккаунта, которое может сохраняться и после остановки OpenFlux.
> Считайте MAX-транспорт экспериментальным до выяснения механизма блокировки.

## Важные ограничения

OpenFlux - экспериментальный исследовательский проект, а не проверенная замена
WireGuard или другому зрелому VPN. Android-туннель сейчас поддерживает IPv4 и
TCP; произвольный UDP и IPv6 через туннель не передаются (запрос с IPv6-адресом
или не-CONNECT командой отклоняется корректной ошибкой протокола, а не тихо
зависает). Владелец транспорта по-прежнему видит метаданные: время
соединения, объём трафика и зашифрованные данные. Пользователь с правом
редактирования документа может нарушить доступность соединения.

Используйте программу только на своих системах и сетях либо там, где у вас есть
разрешение на тестирование.

## Требования

- Go 1.26.4 или новее для клиента компьютера и выходной ноды;
- Linux VPS/VDS с root-доступом для выходной ноды;
- для сборки Android: Java 17, Android SDK/API 35, Build Tools 35.0.0,
  NDK 27.0.12077973, Gradle 8.14.3 и `gomobile`;
- редактируемый документ в старом редакторе Yandex Docs при использовании
  транспорта Yandex.

## Подготовка приватной конфигурации

Создайте эти файлы локально и передайте те же значения на выходную ноду. Они
исключены через `.gitignore`, их нельзя добавлять в Git:

```bash
printf '%s\n' 'https://ссылка-на-ваш-документ' > document-url
openssl rand -base64 32 > encryption-key
chmod 600 document-url encryption-key
```

Ключ шифрования опционален: не передавайте `--encryption-key-file` на обеих
сторонах (и оставьте поле ключа пустым в Android-приложении), чтобы
подключаться к обычной, немодифицированной ноде апстрима без шифрования
транспорта. Если решите использовать ключ - он должен содержать не менее 16
символов, быть уникальным случайным значением, а не обычным паролем, и
совпадать на обеих сторонах. Если ссылка или ключ раскрыты, замените оба
значения.

## Сборка ноды и клиента компьютера

Выходная нода и клиент компьютера - один и тот же бинарник, различаются только
флаги запуска. Готовые Linux-бинарники `amd64`/`arm64` прикладываются к каждому
[релизу на GitHub](https://github.com/damnurmum/OpenFlux-Android/releases/latest)
рядом с Android APK. Чтобы собрать самостоятельно:

```bash
go build -o openflux .
```

По умолчанию нода работает в `--mode proxy` - без root, на любой ОС, ходит в
интернет обычным `net.Dial` вместо raw-сокета. Весь RST-drop ниже ей не
нужен. Передайте `--mode raw` для старого raw-сокет пути (только Linux,
нужен root) - весь этот раздел про него.

TCP-соединения выходной ноды живут в userspace-стеке (gvisor), у ядра нет для
них сокета, и оно слало бы RST на каждый ответный пакет - туннель бы рвался.
Этот RST надо подавить, но точечно, не на весь хост. Глухое `-j DROP` на все
исходящие RST превращает закрытые порты в «молчащие» (сканер видит `filtered`
вместо `closed`) и мешает хосту нормально сбрасывать посторонние соединения.

Рекомендуется: выделите машине второй/алиас IP под туннель и ограничьте
правило им через `--local-ip`:

```bash
sudo iptables -A OUTPUT -p tcp --tcp-flags RST RST -s 203.0.113.10 -j DROP
sudo ./openflux --exit-node --transport yandex --local-ip 203.0.113.10 \
  --url-file ./document-url --encryption-key-file ./encryption-key
```

Ещё чище - запускать ноду в отдельном network namespace или контейнере, тогда
правило вообще не трогает остальные сервисы хоста. `-m owner --uid-owner` тут
не работает: рвущие туннель RST генерирует ядро без сокета-владельца, и
owner-матч не срабатывает.

Запасной вариант на весь хост (только на однозадачной машине, с пониманием
последствий выше):

```bash
sudo iptables -C OUTPUT -p tcp --tcp-flags RST RST -j DROP 2>/dev/null || \
  sudo iptables -I OUTPUT 1 -p tcp --tcp-flags RST RST -j DROP
sudo ./openflux --exit-node --transport yandex \
  --url-file ./document-url --encryption-key-file ./encryption-key
```

Пример [systemd-сервиса](deploy/openflux.service) ожидает бинарник и приватные
файлы в `/root/openflux`. Перед установкой проверьте пути:

```bash
sudo install -d -m 700 /root/openflux
sudo install -m 755 ./openflux /root/openflux/openflux
sudo install -m 600 ./document-url ./encryption-key /root/openflux/
sudo install -m 644 deploy/openflux.service /etc/systemd/system/openflux.service
sudo systemctl daemon-reload
sudo systemctl enable --now openflux
sudo systemctl status openflux
```

Запустите клиент компьютера и настройте в браузере SOCKS5-прокси
`127.0.0.1:1080`:

```bash
./openflux --client --transport yandex --socks5 127.0.0.1:1080 \
  --url-file ./document-url --encryption-key-file ./encryption-key
```

Добавляйте `--debug` только при диагностике и проверяйте логи перед публикацией.

## Сборка и установка Android-приложения

Укажите `ANDROID_SDK_ROOT` (или `ANDROID_HOME`), установите `gomobile` и Gradle,
затем выполните:

```bash
go install golang.org/x/mobile/cmd/gomobile@v0.0.0-20260908204917-8b95e45f8d3e
go install golang.org/x/mobile/cmd/gobind@v0.0.0-20260908204917-8b95e45f8d3e
gomobile init
./build_android_app.sh
```

Сборка создаёт отдельные APK для `arm64-v8a`, `armeabi-v7a`, `x86_64` и `x86`, а
также `OpenFlux-android-universal-debug.apk` для устройств с неизвестной
архитектурой. Передайте подходящий APK на устройство с Android 8+, установите,
укажите собственные ссылку и общий секрет во вкладке **«Настройки»**, затем
подтвердите системный запрос Android на создание VPN.

Настройки сохраняются после обычного обновления приложения, если Application ID
и сертификат подписи не менялись. Очистка данных или удаление приложения стирает
их. APK с другим сертификатом не сможет обновить установленную версию. Артефакты
CI подписаны debug-ключом, а APK в GitHub Releases - постоянным release-ключом.
При переходе с debug на release приложение потребуется один раз удалить, поэтому
сохранённые настройки будут очищены.

Дополнительные сведения находятся в [android/README.md](android/README.md).

## Флаги командной строки

| Флаг | По умолчанию | Описание |
| --- | --- | --- |
| `--client` | выкл. | Запустить SOCKS5-клиент |
| `--exit-node` | выкл. | Запустить выходную ноду |
| `--mode` | `proxy` | Способ доступа в интернет у ноды: `proxy` (по умолчанию, работает везде, без root) или `raw` (только Linux, нужен root; сам откатится на proxy, если не смог получить raw-сокет) |
| `--local-ip` | пусто | Egress IP выходной ноды - для точечного RST-drop правила (только режим raw) |
| `--socks5` | `:1080` | Адрес SOCKS5-прокси |
| `--transport` | `yandex` | Транспорт (`yandex`, `vyandex`, `oneme` или `cupsonline`) |
| `--url` | пусто | Ссылка в аргументе; безопаснее `--url-file` |
| `--url-file` | пусто | Прочитать ссылку на документ из файла |
| `--encryption-key-file` | пусто | Опционально: зашифровать транспорт секретом из файла |
| `--maxToken` | пусто | Токен транспорта MAX |
| `--maxUid` | пусто | ID пользователя транспорта MAX |
| `--debug` | выкл. | Включить подробные логи |

`cupsonline` - экспериментальный транспорт поверх фичи "курсор соседа" в
комнатах live-coding интервью cups.online. На момент написания не доставляет
трафик надёжно (похоже, канал рассылки курсора троттлит/схлопывает частые
обновления) - считайте неподдерживаемым, пока апстрим не починит.

## Разработка и безопасность

Перед коммитом выполните:

```bash
gofmt -w $(git ls-files '*.go')
go test ./...
go vet ./...
git diff --check
```

Правила участия находятся в [CONTRIBUTING.md](docs/CONTRIBUTING.md), порядок
сообщения об уязвимостях - в [SECURITY.md](docs/SECURITY.md), список
изменений - в [CHANGELOG.md](docs/CHANGELOG.md).

## Лицензия

OpenFlux распространяется по GNU General Public License v3.0 или более поздней
версии. См. [LICENSE](LICENSE), [COPYRIGHT](COPYRIGHT) и [NOTICE](NOTICE). Этот
форк не одобрен Yandex и не связан с компанией.
