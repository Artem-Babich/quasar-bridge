# Классификатор маршрутов на OpenAI Decisions

Это инструкция по эксплуатации для форка. Описание полей есть в
[guide.md](guide.md#классификатор-на-openai-decisions-форк).

Схема работы: правила → Decisions → (если Decisions не ответил) LLM-классификатор → `on_error`.
Весь шаг классификации занимает не больше `routing.classifier.timeout_ms`.

## Включить

Включайте через `runtime.env`, а `config.json` не трогайте. Тогда прошлый релиз
продолжит читать тот же конфиг, и откат сводится к замене бинарника.

Перед установкой нового бинарника сохраните прошлый, он понадобится для отката:

```sh
sudo cp -p /opt/quasar-bridge/quasar-bridge /opt/quasar-bridge/quasar-bridge.prev
```

Сохраните копию `runtime.env` и откройте его редактором. Ключ не набирайте в
командной строке, иначе он останется в истории оболочки.

```sh
sudo cp -p /etc/quasar-bridge/runtime.env /etc/quasar-bridge/runtime.env.pre-decisions
sudoedit /etc/quasar-bridge/runtime.env
```

Добавьте строки:

```sh
QUASAR_CLASSIFIER_API=decisions
QUASAR_CLASSIFIER_BASE_URL=https://api.openai.com/v1
QUASAR_CLASSIFIER_MODEL=gpt-6-luna
QUASAR_CLASSIFIER_API_KEY=sk-...
```

Ключ лучше взять из отдельного проекта OpenAI с лимитом расходов.
Без ключа мост с `decisions` не запустится.

## Проверить

Мост перезапускается, только если все три проверки прошли:

```sh
sudo -u quasar-bridge sh -c 'set -a; . /etc/quasar-bridge/runtime.env; set +a; \
  /opt/quasar-bridge/quasar-bridge -config /etc/quasar-bridge/config.json -check-config && \
  /opt/quasar-bridge/quasar-bridge -config /etc/quasar-bridge/config.json -check-backends && \
  /opt/quasar-bridge/quasar-bridge -config /etc/quasar-bridge/config.json -route "сочини стих про кота"' \
  && sudo systemctl restart quasar-bridge \
  && sudo journalctl -u quasar-bridge -n 20 -o cat | grep classifier_backend
```

Если проверка не прошла, мост продолжает работать по-старому. Верните прежний
`runtime.env`, чтобы ошибка не всплыла при следующем перезапуске или перезагрузке:

```sh
sudo cp -p /etc/quasar-bridge/runtime.env.pre-decisions /etc/quasar-bridge/runtime.env
```

Текст ошибки подсказывает причину: `Decisions http 401 invalid_api_key` означает
неверный ключ, `http 404` неверный адрес или модель, а `http 429` лимит запросов
или расходов.

- В `-check-backends` должно быть
  `Classifier: decisions ok, route …, confidence …, cold N ms new connection, warm M ms reused connection (timeout 1500 ms)`.
  `timeout` — лимит Decisions: `decisions_timeout_ms` (0 = 1500 мс).
  Если у `cold` стоит `reused connection`, адрес LLM и Decisions на одном хосте,
  и холодный замер на самом деле тёплый.
- Если там `WARNING: cold call exceeds decisions_timeout_ms`, то после простоя
  Decisions будет часто не успевать, и маршрут будет выбирать LLM. Увеличьте
  `routing.classifier.decisions_timeout_ms` (применяется по `systemctl reload`);
  значение должно быть меньше `timeout_ms`, иначе конфиг не примется.
  Применилось ли — видно в журнале:
  `sudo journalctl -u quasar-bridge -n 20 -o cat | grep -E 'routing_reload|classifier_backend'`.
  Нужны `routing_reloaded` и `classifier_backend` с новым `decisions_timeout_ms`;
  `routing_reload_rejected` значит, что конфиг не принят и работает прежний.
  ⚠️ Это поле в `config.json`: перед откатом на старый бинарник его нужно убрать
  (см. «Откатить бинарник»).
- В выводе `-route` должно быть `"backend":"decisions"`.

## Смотреть данные

Каждое решение классификатора попадает в строку `native_route_decision`. Её поля:
`backend`, `latency_ms`, `confidence`, `decisions_ms`, `conn_reused`, а при сбое
Decisions ещё `error_class`, `error_code` и `request_id`.

```sh
# journalctl выдаёт и служебные строки systemd, поэтому jq читает строки как текст
decisions() { sudo journalctl -u quasar-bridge --since today -o cat | jq -cR 'fromjson? | select(.msg=="native_route_decision")'; }
# медиана времени Decisions, мс
decisions | jq -r 'select(.backend=="decisions") | .decisions_ms' | sort -n | awk '{a[NR]=$1} END{print a[int((NR+1)/2)]}'
# сколько раз Decisions не ответил, по причинам
decisions | jq -r 'select(.error_class) | .error_class' | sort | uniq -c
# доля решений Decisions среди всех решений классификатора
decisions | jq -s '[.[] | select(.backend)] | {all: length, decisions: map(select(.backend=="decisions")) | length}'
# неуверенные решения (ниже 0.7) с тегом запроса
decisions | jq -c 'select(.backend=="decisions" and .confidence < 0.7) | {request, route, confidence}'
# доля холодных соединений
decisions | jq -s '[.[] | select(.conn_reused != null)] | {all: length, cold: map(select(.conn_reused==false)) | length}'
```

Чтобы узнать фразу неуверенного решения, нужен журнал разговоров
(`conversation_log`). Тег `request` в логе (8 hex) совпадает с началом поля
`request` в журнале (32 hex):

```sh
sudo jq -c 'select(.event=="transcript" and (.request | startswith("ТЕГ"))) | .text' /var/log/quasar-bridge/conversations.jsonl
```

## Выключить

Задайте пустое значение `QUASAR_CLASSIFIER_API=` (пустая переменная тоже
перекрывает JSON) или удалите строки из `runtime.env`. Затем:

```sh
sudo systemctl restart quasar-bridge
```

Маршрут снова выбирает LLM-классификатор, как до форка.

## Откатить бинарник

Если `config.json` не менялся, достаточно вернуть прошлый бинарник. Старый мост
переменные `QUASAR_CLASSIFIER_*` игнорирует. `install` заменяет файл целиком,
поэтому работает и пока мост запущен (обычный `cp` на запущенный файл завершится
ошибкой «Text file busy»).

```sh
sudo install -m 755 /opt/quasar-bridge/quasar-bridge.prev /opt/quasar-bridge/quasar-bridge \
  && sudo -u quasar-bridge /opt/quasar-bridge/quasar-bridge -config /etc/quasar-bridge/config.json -check-config \
  && sudo systemctl restart quasar-bridge
```

⚠️ Если поля `classifier_*` или `decisions_timeout_ms` всё же попали в
`config.json`, сначала уберите их. Старый бинарник отклоняет незнакомые поля,
не запускается, и колонка остаётся без моста.
