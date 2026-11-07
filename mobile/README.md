# Flutter app

This folder contains the Flutter UI source and Go API client.

## Create platform runners (once)

With Flutter 3.24 or newer installed, run from this folder:

```sh
flutter create --platforms=android,ios,web .
flutter pub get
```

The app sources are in `lib/`; this command creates Flutter's Android, iOS, and web runner folders.
For Android development against the local HTTP API, make sure the generated `android/app/src/main/AndroidManifest.xml` grants `android.permission.INTERNET` and enables cleartext traffic on the `<application>` element (`android:usesCleartextTraffic="true"`). Use HTTPS and remove that development allowance for a deployed service.

## Run with the Go backend

In one terminal, from the repository root:

```sh
./scripts/run.sh
```

In another terminal, from `mobile/`:

```sh
flutter run -d chrome
```

For Android Emulator, use `flutter run` with the emulator started. The default backend URL is `http://10.0.2.2:8080`. Chrome and iOS Simulator default to `http://localhost:8080`. On a physical phone, open **Операции → Адрес сервера**, enter the computer's LAN IP (for example `http://192.168.1.20:8080`), and allow the port through the computer firewall.

The app connects to `GET /health`, `GET /api/demo`, and `POST /api/analyze`. Document uploads are sent as multipart form data; the API accepts PDF, DOCX, and TXT files up to 15 MB each, with a 15 MB request limit.

## Tests

```sh
flutter test
flutter analyze
```

The widget test checks that **Подписать** stays disabled until the person accepts the terms. The signature screen is a demo confirmation; this prototype does not create a bank payment or apply a digital signature.
