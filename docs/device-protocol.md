# Reader ↔ backend protocol

For the embedded team. The authoritative request/response schemas are the
`/device/*` operations in [`api/openapi.yaml`](../api/openapi.yaml); this page
explains how to use them.

```
[Card / phone] ⇄ NFC frontend (PN5180 / PN512 / ST25R3916) ⇄ STM32 ⇄ UART/SPI ⇄ ESP32-C3 ⇄ Wi-Fi ⇄ backend
                                                                            │
                                                         HTTPS (requests) ──┤
                                                         MQTT  (commands) ──┘
```

## 1. Provisioning

1. An admin registers the reader: `POST /api/admin/readers` with its serial.
2. The response contains `api_key` (`fdr_...`) **once**. Store it in the
   reader's protected flash (ESP32 NVS with flash encryption enabled).
3. If a key leaks, the admin calls `POST /api/admin/readers/{id}/rotate-key`;
   the old key stops working immediately.

Every HTTP request sends `Authorization: Bearer fdr_...`.

## 2. Boot and heartbeat

`POST /api/device/heartbeat` `{ "firmware_version": "1.2.0" }` at boot and
every 30–60 s. The response gives:

- `server_time` — set the RTC from it (needed for `occurred_at`).
- `reader` — its `mode` (`access` or `wallet`) and `zone_id`.
- `mqtt_topic` — subscribe to it (below).
- `hce_aid` — the AID to SELECT when a phone is presented.

`403` with code `reader_disabled` means an admin disabled the reader: show an
"out of service" state and retry the heartbeat slowly (e.g. every 5 min).

## 3. Card taps (`mode = access`)

`POST /api/device/taps`

```json
{
  "uid": "04A2B3C4D5E680",
  "kind": "desfire",
  "secure_auth": true,
  "event_id": "0193c2a4-....",
  "occurred_at": "2026-11-24T09:15:02Z"
}
```

- `uid` — hex, 4–32 bytes.
- `kind` — `desfire`, `mifare_classic`, `mifare_ultralight`, `ntag`, `iso15693`, `mobile_hce`.
- `secure_auth` — `true` only if the reader completed DESFire AES mutual
  authentication with the card's **diversified** key (NXP AN10922). Zones
  marked "require secure credential" deny everything else.
- `event_id` — a UUID the reader generates per tap. If the request times out,
  **retry with the same `event_id`**: you get the original decision and the
  tap is logged only once.

Response (always `200`):

```json
{ "event_id": "...", "decision": "granted", "reason": "granted",
  "unlock_ms": 3000, "display_name": "Ada Lovelace" }
```

Open the lock for `unlock_ms` only when `decision == "granted"`. For any
other value show a generic "access denied"; `reason` is for logs/LEDs, not
for the cardholder.

`decision == "enrolled"` means the reader was armed for enrollment and the
card was just registered: give a distinct beep/LED.

If the backend is unreachable: **fail closed** (do not open), show an error
LED, and retry later. Offline allow-lists are out of scope for now.

## 4. Phones (HCE, `kind = mobile_hce`)

The mobile app emulates an ISO 14443-4 card under the AID from the heartbeat.

1. Reader: `SELECT AID <hce_aid>`.
2. Reader sends a challenge APDU containing 16–32 random bytes **and the
   reader id (16 bytes, the UUID in binary)**.
3. Phone returns its credential `uid` (16 bytes) and
   `HMAC-SHA256(secret, challenge || reader_id_bytes)` (32 bytes).
4. Reader calls `/device/taps` with `kind: "mobile_hce"`, `uid`, and
   `mobile: { "challenge": "<hex>", "response": "<hex>" }`.

The backend verifies the HMAC; binding the reader id prevents a response
captured at one door being replayed at another. The exact APDU layout is to
be agreed between the embedded and mobile teams and documented here.

## 5. Payments (`mode = wallet`)

`POST /api/device/wallet/charges`

```json
{ "uid": "04A2B3C4D5E680", "kind": "desfire", "secure_auth": true,
  "amount_minor": 2500, "description": "Coffee", "idempotency_key": "rdr1-000123" }
```

`amount_minor` is in kuruş (2500 = 25.00 TRY). Generate a unique
`idempotency_key` per purchase and reuse it on retries — the customer is
charged at most once. Response `approved: true|false`, `reason`
(`insufficient_funds`, `wallet_frozen`, ...) and `balance_minor`.

## 6. Commands (server → reader)

Admins can queue commands such as **writing data to the next presented card**.

- **MQTT (push):** connect to the backend host, port 1883 (8883 with TLS),
  `client id` = anything unique, `username` = reader id, `password` = API key.
  Subscribe (QoS 1) to the `mqtt_topic` from the heartbeat. Each message is
  the JSON `ReaderCommand` from the contract. Readers cannot publish.
- **HTTP (poll, fallback):** `GET /api/device/commands` returns open commands
  and marks them delivered. Poll every heartbeat if MQTT is down.

After executing, report: `POST /api/device/commands/{id}/result`
`{ "status": "succeeded" | "failed", "result": { ... } }`. For `read_card`
put the data in `result.data_hex`; on failure put a short `result.error`.
Commands expire (`expires_at`); do not execute expired ones.

`write_card` / `read_card` payload (`WriteCardPayload` in the contract):

| Field | Meaning |
|---|---|
| `expected_uid` | only act on this card (optional) |
| `aid`, `file_no`, `offset` | DESFire application / file / offset |
| `block` | MIFARE Classic block or NTAG page |
| `length` | bytes to read |
| `data_hex` | bytes to write |

## 7. Errors

Non-2xx responses are RFC 9457 `application/problem+json` with a stable
`code` and a `request_id` — log both. `401` = bad key, `403` = reader
disabled, `400` = request does not match the contract (check `errors[]`).
