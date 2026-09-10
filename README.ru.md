# OpenFlux

[English](README.md) | **Русский**

> Это экспериментальный независимо развиваемый форк
> [p1neappleXpress/OpenFlux](https://github.com/p1neappleXpress/OpenFlux).
> Основные отличия от исходного проекта перечислены в [FORK.md](FORK.md).

OpenFlux — исследовательский TCP-туннель с подключаемыми транспортами. В этом
форке добавлены Android VPN-клиент и обязательное сквозное шифрование для
транспорта через Yandex Docs.

```text
Android VPN или SOCKS5-клиент -> зашифрованный транспорт -> Linux-нода -> интернет
```

## Возможности

- Android-клиент для Android 8+ (`arm64-v8a`) на системном `VpnService`;
- интерфейс в стиле Android 11 с подключением, логами и настройками;
- аутентифицированное шифрование AES-256-GCM и получение ключа через scrypt;
- хранение ссылки и общего секрета с защитой Android Keystore;
- зашифрованная проверка задержки и живой график пинга;
- DNS-over-HTTPS на Android;
- SOCKS5-клиент для компьютера и режим выходной Linux-ноды;
- транспорт через Yandex Docs и экспериментальный транспорт через MAX.

## Важные ограничения

OpenFlux — экспериментальный исследовательский проект, а не проверенная замена
WireGuard или другому зрелому VPN. Android-туннель сейчас поддерживает IPv4 и
TCP. DNS обслуживается отдельно через HTTPS; произвольный UDP и IPv6 через
туннель не передаются. Владелец транспорта по-прежнему видит метаданные: время
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

Секрет шифрования должен содержать не менее 16 символов. Используйте уникальное
случайное значение, а не обычный пароль. Если ссылка или секрет раскрыты,
замените оба значения.

## Сборка ноды и клиента компьютера

```bash
go build -o openflux .
```

Запустите выходную Linux-ноду от root:

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

Debug APK для arm64 появится в
`dist/OpenFlux-android-arm64-debug.apk`. Передайте его на устройство с Android
8+, установите, укажите собственные ссылку и общий секрет во вкладке
**«Настройки»**, затем подтвердите системный запрос Android на создание VPN.

Настройки сохраняются после обычного обновления приложения, если Application ID
и сертификат подписи не менялись. Очистка данных или удаление приложения стирает
их. APK с другим сертификатом не сможет обновить установленную версию. Артефакт
из CI подписан debug-ключом и предназначен для тестирования, а не для релиза.

Дополнительные сведения находятся в [android/README.md](android/README.md).

## Флаги командной строки

| Флаг | По умолчанию | Описание |
| --- | --- | --- |
| `--client` | выкл. | Запустить SOCKS5-клиент |
| `--exit-node` | выкл. | Запустить выходную ноду (нужен root) |
| `--socks5` | `:1080` | Адрес SOCKS5-прокси |
| `--transport` | `yandex` | Транспорт (`yandex` или `oneme`) |
| `--url` | пусто | Ссылка в аргументе; безопаснее `--url-file` |
| `--url-file` | пусто | Прочитать ссылку на документ из файла |
| `--encryption-key-file` | пусто | Прочитать секрет транспорта Yandex из файла |
| `--maxToken` | пусто | Токен транспорта MAX |
| `--maxUid` | пусто | ID пользователя транспорта MAX |
| `--debug` | выкл. | Включить подробные логи |

## Разработка и безопасность

Перед коммитом выполните:

```bash
gofmt -w $(git ls-files '*.go')
go test ./...
go vet ./...
git diff --check
```

Правила участия находятся в [CONTRIBUTING.md](CONTRIBUTING.md), порядок сообщения
об уязвимостях — в [SECURITY.md](SECURITY.md), список изменений — в
[CHANGELOG.md](CHANGELOG.md).

## Лицензия

OpenFlux распространяется по GNU General Public License v3.0 или более поздней
версии. См. [LICENSE](LICENSE), [COPYRIGHT](COPYRIGHT) и [NOTICE](NOTICE). Этот
форк не одобрен Yandex и не связан с компанией.
