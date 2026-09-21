> RESULT = BlockedByRealAndroidReturnPathRegression
> ReadyForUserAB is revoked. Real Android Baseline at 68e722d42b997c17460d890bfedd83de8b07d234 received zero bytes; ordinary v1.0.0 works with the same profile. Performance A/B is suspended. Prior measurements below are synthetic historical evidence only.

# OpenFlux Perf Lab: ручной A/B на Android

Это экспериментальная сборка, победитель ещё не выбран. VPS, exit binary,
документ и ключ менять не нужно. Wire format, AES и кодеки сохранены.

## Установка

В Actions откройте успешный workflow **Android Performance Lab** именно для
ветки `codex/android-volga-perf-lab` в `GEFSED/OpenFlux`. Скачайте artifact
`OpenFlux-Android-PerfLab-<SHA>` (нужен вход GitHub), распакуйте ZIP.
Для большинства телефонов нужен `OpenFlux-android-arm64-v8a-perflab.apk`;
если архитектура неизвестна — `OpenFlux-android-universal-perflab.apk`.
SHA-256 находятся в `SHA256SUMS`, commit — в `BUILD.txt` и диагностике приложения.
Artifacts хранятся 30 дней. Официальный Release не создаётся.

Название: **OpenFlux Perf Lab**, package: `io.openflux.app.perflab`.
Обычный OpenFlux удалять не нужно. Настройки и Android Keystore изолированы
новым package/UID; настройте соединение в Perf Lab вручную. Не отправляйте
ссылку, ключ, MAX token или скриншот профиля. Сборка подписана тестовым debug
ключом Actions, не production-ключом. Ключ подписи может меняться между runs:
для будущего обновления может потребоваться удалить только Perf Lab.
Android разрешает один активный VPN; перед тестом остановите обычный OpenFlux.
Root телефона не нужен.

## Одинаковые условия

- Один телефон и тот же Wi-Fi; мобильные данные выключены.
- Один exit node, документ, кодек и настройка шифрования во всех прогонах.
- Для основного опыта выберите **Туннель / VPN**, `vyandex`, `batched`.
  Если ваш exit использует `legacy`, оставьте `legacy` во всех прогонах;
  внешний batching тогда не применяется.
- Закройте фоновые загрузки. Держите одинаковую температуру телефона и зарядку.
- На сервере ничего не меняйте.

В редакторе профиля есть **Performance profile (VPN)**. Старые профили и новая
сборка по умолчанию используют **Baseline**. После изменения нажмите
«Сохранить профиль», отключите VPN и подключите снова. Ждите 20–30 секунд.
На главном экране показан выбранный профиль; в диагностике — реально запущенный.
SOCKS5 сохраняет baseline; этот A/B предназначен для VPN.
У других транспортов сетевые параметры не меняются; кандидаты переключают
только Android receive с polling на blocking, что видно в `receive_mode`.

## Порядок

1. Baseline.
2. Balanced.
3. Low latency.
4. Throughput.
5. Baseline повторно — контроль изменения канала за время опыта.

Для каждого выполните три коротких одинаковых download и три upload теста.
Можно вручную использовать <https://speed.cloudflare.com/>; если он запускает
download/upload вместе, три одинаковых полных прогона достаточно.
Начинайте с коротких прогонов, следите за объёмом трафика; десятки гигабайт
не нужны. Никаких массовых автоматических тестов против Yandex.

| Профиль | Download Mbps ×3 | Upload Mbps ×3 | Latency | Паузы / reconnect |
| --- | --- | --- | --- | --- |
| Baseline | | | | |
| Balanced | | | | |
| Low latency | | | | |
| Throughput | | | | |
| Baseline повтор | | | | |

После каждого профиля откройте **«Диагностика производительности»**, подождите
хотя бы два обновления и нажмите **«Копировать диагностику»**. Этот JSON не
содержит URL, ключей, cookies, токенов или packet payload. Сохраните его рядом
с результатами теста. Закрывайте диагностику во время speed test, чтобы
сэмплирование Go heap/PSS каждые 2 секунды не влияло на замер. Метрики upload /
download — локальные счётчики IP-пакетов, не независимый speed test.
Оцените также обычные сайты и видео: зависания, паузы, обрывы.

## Пять минут простоя

Отдельно сравните Baseline и Balanced. Подключите VPN, скопируйте диагностику,
закройте её, ничего не скачивайте пять минут, снова скопируйте диагностику.
Сравните разницу `process_cpu_time_ms`, `read_calls`, `read_wait_calls`, heap,
PSS, goroutines, drops и reconnects. CPU time охватывает весь процесс, включая
UI и DNS; это не только Go. `process_cpu_percent_one_core` показывается после
второго сэмпла и может превышать 100% при работе нескольких ядер.
Одинаковое состояние экрана и фоновых приложений важно.

Присылайте таблицу и JSON, модель телефона / Android, тип сети, наличие пауз.
Не присылайте секреты или полные логи. Победитель определяется только после
реального A/B: улучшение median throughput/p95-пауз/памяти/idle CPU не должно
сопровождаться drops, crash, reconnect или ухудшением другого направления.
