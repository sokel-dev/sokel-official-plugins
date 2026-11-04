# Umeng Push (U-Push)

Use Umeng for app push notifications: unicast or listcast to device tokens, or broadcast to all devices. It complements
mobile push in the Alibaba Cloud plugin, with each channel serving its own device pool in a dual-channel architecture.

## 1. Prepare credentials

Open the [Umeng console](https://message.umeng.com/) → Application Management. **Android and iOS are separate applications**,
each with an AppKey / Master Secret pair. Fill the credential group for each platform you use; configure only one if you target only that platform.

## 2. Usage

- **Push**: separate multiple `device_tokens` with commas, up to 500. **Empty means broadcast to all devices**; choose deliberately before sending.
  Android tokens are 44 characters; iOS tokens are 64 hexadecimal characters. With `production` off, iOS uses the test-certificate environment.
- **Get task status**: broadcast/listcast returns a task ID for querying delivery and open counts.
  **Unicast is a message-type push and Umeng provides no task statistics**; it returns msg_id instead. This is Umeng's API behavior.
- **Cancel task**: only tasks that have not started sending, such as scheduled or queued tasks, can be canceled.

## 3. Troubleshooting

| Error | Resolution |
|---|---|
| Umeng rejects the request | Check AppKey/Master Secret. Android and iOS are separate applications; do not mix their credentials. |
| Invalid device_token | Android requires 44 characters; iOS requires 64 hexadecimal characters. Do not use another push provider's token. |
| iOS does not receive messages | Match production to the signing-certificate environment and inspect the iOS certificate configuration in the Umeng console. |
| Android does not receive messages | Configure vendor channels such as Huawei or Xiaomi in the Umeng console. With only the Umeng channel, delivery commonly fails after the app is killed in the background. |
