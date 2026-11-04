# SUBMAIL SMS

Send **domestic Chinese SMS** and **international SMS** through SUBMAIL for alerts, verification codes, and customer outreach.

## 1. Prepare credentials

1. Open the [SUBMAIL console](https://www.mysubmail.com/console) → Application Integration → Create application.
   **Domestic SMS and International SMS are separate applications**, each with its own AppID/AppKey. Create the ones you need.
2. Enter each AppID/AppKey in its corresponding credential group. If you use only one service, fill only that group.
3. For domestic SMS, register the **sender signature**, such as 【Example Technology】, in the console first. For template sending, submit the **template** for approval too.
4. If the application has an IP allowlist, add the plugin's outbound IP or disable the allowlist.

## 2. Operation reference

| Operation | Purpose |
|---|---|
| Domestic SMS · Send | Send full text directly. Content **must contain an approved 【sender signature】**. Separate multiple phone numbers with commas. |
| Domestic SMS · Send template | Template ID plus variables such as `{"code":"123456"}`. Recommended for verification codes and notifications because approval is faster and delivery is consistent. |
| International SMS · Send / Send template | Numbers include `+country code`, such as +81 or +1. Uses the international SMS application. |
| Get balance | Domestic balance is measured in messages; international balance is monetary. **They are queried separately** according to the credential groups configured. Schedule a check and alert below a threshold. |

## 3. Troubleshoot missing messages

A successful send response with a send_id means SUBMAIL **accepted the request**, not that the phone received it. Check delivery status:

1. Use Domestic SMS · Get delivery status with send_id or a phone number.
   `delivered` means delivered, `dropped` means discarded by the carrier with a reason in report, and `sending` means in transit.
2. Common dropped reasons include an **unregistered signature**; the 【signature】 in the message must exactly match the approved console entry.
   Other causes include content risk controls such as links or marketing terms, invalid numbers, or blocklists.
3. New accounts whose qualifications are not yet approved can send only to test numbers registered in the console.
4. If everything else is normal, check the phone's spam folder.

International SMS has no corresponding status-query interface here. Check delivery in the SUBMAIL console.

## 4. Common combinations

- **SMS alerts**: alert event → Domestic SMS · Send template with an on-call template and variables.
- **Balance monitoring**: every day at 09:00 → Get balance → `balance < 100` → Send a Feishu reminder to top up.
- **Overseas notifications**: route by country code using a condition node → Domestic or international SMS.

## 5. Troubleshooting

| Error | Resolution |
|---|---|
| AppID/AppKey not recognized | Domestic and international SMS use **separate applications**. Do not put domestic credentials in the international group. |
| Missing SMS signature | Add the approved 【signature】 at the beginning of the content. |
| Template missing or not approved | Templates must be approved before use. Verify the template ID. |
| IP not allowlisted | Add the plugin's outbound IP in the application's console settings. |
| Insufficient balance | Top up. A balance-monitoring workflow is recommended. |

## 6. Security

AppKey is used only inside the plugin, not in node inputs, outputs, or logs. SMS is billed per message.
Consider placing a human approval node before sending, especially before external bulk messages.
