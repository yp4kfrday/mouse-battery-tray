# ⚡ MouseBatteryTray (v0.1.0-beta)

**MouseBatteryTray** — сверхлёгкая, полностью автономная утилита для системного трея Windows на чистом Go (Zero CGO, Zero MinGW, без фонового Node.js/Electron и без сторонних библиотек), предназначенная для честного мониторинга уровня заряда и статуса питания беспроводных игровых мышей в реальном времени.

---

## 🎮 Поддерживаемые семейства мышей

Все поддерживаемые модели описаны в конфигурационном файле [`devices.json`](devices.json):

1. **Ajazz / CompX 8K Wireless:**
   - Ajazz AJ179 Pro (`VID: 0x3151, PID: 0x402D`)
   - Ajazz AJ159 Pro / Apex (`VID: 0x3151, PID: 0x402E`)
   - Ajazz AJ139 Pro (`VID: 0x3151, PID: 0x4018`)
   - Беспроводные донглы CompX 8K (`VID: 0x25A7`) и SinoWealth (`VID: 0x258A`)
2. **VXE / VGN / ATK / Nordic 52:**
   - VXE R1 SE+ (`VID: 0x3554, PID: 0xF58E`)
   - VXE R1 SE (`VID: 0x3554, PID: 0xF58F`)
   - VXE R1 Pro / Pro Max (`VID: 0x3554, PID: 0xF58C`)
   - VXE Mad R / Major (`VID: 0x3554, PID: 0xF58D`)
   - VGN Dragonfly F1 Pro / MOBA (`VID: 0x3554, PID: 0xF55B`)
   - VGN Dragonfly F1 4K (`VID: 0x3554, PID: 0xF505`)
   - ATK Blazing Sky F1 / X1 (`VID: 0x373B`)
3. **Logitech Lightspeed & HID++ 2.0:**
   - Logitech G Pro Wireless / Superlight (`VID: 0x046D, PID: 0xC539`)
   - Logitech G Pro X Superlight 2 (`VID: 0x046D, PID: 0xC547`)
   - Logitech G305 / G304 Lightspeed (`VID: 0x046D, PID: 0xC53F`)
   - Logitech MX Master 3 / 3S (`VID: 0x046D, PID: 0xB034`)
4. **Razer Wireless:**
   - Razer Viper Ultimate / V2 Pro / V3 Pro (`VID: 0x1532`)
   - Razer DeathAdder V2 Pro / V3 Pro (`VID: 0x1532`)
   - Razer Basilisk Ultimate / V3 Pro (`VID: 0x1532`)
5. **SteelSeries Wireless:**
   - SteelSeries Aerox 3 / 5 / 9 Wireless (`VID: 0x1038`)
   - SteelSeries Rival 3 / 650 Wireless (`VID: 0x1038`)
6. **Windows PnP / Bluetooth LE:**
   - Любая Bluetooth LE мышь, передающая заряд в стандартный дескриптор Windows `DEVPKEY_Device_BatteryLevel`.

---

## 🔄 Автоматическое самообновление (Self-Updating)

В программу встроен бесшовный механизм обновления через **GitHub Releases API**:

- **Как это работает:**
  1. При запуске (через 10 секунд задержки) и каждые 2 часа в фоне проверяется наличие свежих релизов (включая пре-релизы и бета-версии, например `v0.1.1-beta`).
  2. Также обновление можно запустить вручную в любой момент: правый клик по иконке $\rightarrow$ **«Проверить обновления»**.
  3. Если найдена более новая версия, утилита скачивает `mouse-battery.exe.new`.
  4. Запущенный бинарник переименовывается в `.old`, новый файл занимает его место, после чего процесс перезапускается без разрыва работы трея.
  5. При следующем запуске файл `.old` удаляется автоматически.

---

## 📋 Контекстное меню трея (правый клик)

1. **Строка статуса** (неактивная) — текущее устройство, заряд и статус (например, `VXE R1 SE+: 82% (Разряжается)` или `Ajazz AJ179 Pro: 100% (Заряжается ⚡)`).
2. `Обновить сейчас` — мгновенный повторный опрос устройств без ожидания таймера.
3. `Проверить обновления` — принудительная проверка свежего релиза на GitHub.
4. `Открыть лог` — открытие файла диагностики `mouse-battery.log` в блокноте.
5. `Запускать вместе с Windows` — автозагрузка через реестр `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`.
6. `Выход` — корректное закрытие приложения с удалением иконки из трея.

---

## 📜 Диагностический лог (`mouse-battery.log`)

Лог ведётся с точностью до миллисекунд и содержит:

- Отправленные и принятые HID-пакеты (raw hex);
- Распарсенные проценты заряда, флаги зарядки от док-станции/провода;
- Статусы сетевых запросов самообновления;
- Авто-ротация при достижении 500 КБ (защита от разрастания файла).

---

## 🏗 Сборка и релиз

### Локальная компиляция

Требуется установленный Go 1.21+:

```powershell
# В Windows PowerShell:
.\build.ps1 -Version "v0.1.0-beta"
```

```bash
# В Linux / WSL:
./build.sh v0.1.0-beta
```

### Автоматический релиз на GitHub (GitHub Actions)

В репозиторий добавлен воркфлоу [`.github/workflows/release.yml`](.github/workflows/release.yml):

- При пуше тега версии (`git tag v0.1.0-beta && git push origin v0.1.0-beta`) GitHub Actions автоматически:
  1. Собирает оптимизированный бинарник `mouse-battery.exe` под Windows amd64;
  2. Вшивает номер версии в бинарник;
  3. Создаёт GitHub Release (с пометкой `pre-release` для бета-версий);
  4. Прикрепляет `mouse-battery.exe` и `devices.json` к релизу.
- Также доступен ручной запуск сборки из вкладки **Actions** (workflow_dispatch).
