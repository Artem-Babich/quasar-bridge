# Классификатор маршрутов на OpenAI Decisions

Это инструкция по эксплуатации для форка. Описание полей есть в
[guide.md](guide.md#классификатор-на-openai-decisions-форк).

Схема работы: правила → Decisions → (если Decisions не ответил) LLM-классификатор → `on_error`.
Весь шаг классификации занимает не больше `routing.classifier.timeout_ms`.

## Включить

Включайте через `runtime.env`, а `config.json` не трогайте. Тогда прошлый релиз
продолжит читать тот же конфиг, и откат сводится к замене бинарника.

```sh
sudo cp /etc/quasar-bridge/runtime.env /etc/quasar-bridge/runtime.env.pre-decisions
sudo tee -a /etc/quasar-bridge/runtime.env >/dev/null <<'EOF'
QUASAR_CLASSIFIER_API=decisions
QUASAR_CLASSIFIER_BASE_URL=https://api.openai.com/v1
QUASAR_CLASSIFIER_MODEL=gpt-6-luna
QUASAR_CLASSIFIER_API_KEY=sk-...
EOF
```

Ключ лучше взять из отдельного проекта OpenAI с лимитом расходов.
Без ключа мост с `decisions` не запустится.

## Проверить

```sh
sudo -u quasar-bridge sh -c 'set -a; . /etc/quasar-bridge/runtime.env; set +a; \
  /opt/quasar-bridge/quasar-bridge -config /etc/quasar-bridge/config.json -check-config && \
  /opt/quasar-bridge/quasar-bridge -config /etc/quasar-bridge/config.json -check-backends && \
  /opt/quasar-bridge/quasar-bridge -config /etc/quasar-bridge/config.json -route "сочини стих про кота"'
sudo systemctl restart quasar-bridge
sudo journalctl -u quasar-bridge -n 20 -o cat | grep classifier_backend
```

- В `-check-backends` должно быть
  `Classifier: decisions ok, route …, confidence …, cold N ms, warm M ms (timeout 1500 ms)`.
- Если там `WARNING: cold call exceeds decisions_timeout_ms`, то после простоя
  Decisions будет часто не успевать, и маршрут будет выбирать LLM. Увеличьте
  `routing.classifier.decisions_timeout_ms` (применяется по `systemctl reload`).
- В выводе `-route` должно быть `"backend":"decisions"`.

## Смотреть данные

Каждое решение классификатора попадает в строку `native_route_decision`. Её поля:
`backend`, `latency_ms`, `confidence`, `decisions_ms`, `conn_reused`, а при сбое
Decisions ещё `error_class`, `error_code` и `request_id`.

```sh
J='journalctl -u quasar-bridge --since today -o cat'
# медиана времени Decisions, мс
$J | jq -r 'select(.msg=="native_route_decision" and .backend=="decisions") | .decisions_ms' | sort -n | awk '{a[NR]=$1} END{print a[int((NR+1)/2)]}'
# сколько раз Decisions не ответил, по причинам
$J | jq -r 'select(.msg=="native_route_decision" and .error_class) | .error_class' | sort | uniq -c
# доля решений Decisions среди всех решений классификатора
$J | jq -s '[.[] | select(.msg=="native_route_decision" and .backend)] | {all: length, decisions: map(select(.backend=="decisions")) | length}'
# неуверенные решения (ниже 0.7) с тегом запроса
$J | jq -c 'select(.msg=="native_route_decision" and .backend=="decisions" and .confidence < 0.7) | {request, route, confidence}'
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
переменные `QUASAR_CLASSIFIER_*` игнорирует.

```sh
sudo cp /opt/quasar-bridge/quasar-bridge.prev /opt/quasar-bridge/quasar-bridge
sudo -u quasar-bridge /opt/quasar-bridge/quasar-bridge -config /etc/quasar-bridge/config.json -check-config
sudo systemctl restart quasar-bridge
```

⚠️ Если поля `classifier_*` или `decisions_timeout_ms` всё же попали в
`config.json`, сначала уберите их. Старый бинарник отклоняет незнакомые поля,
не запускается, и колонка остаётся без моста.
