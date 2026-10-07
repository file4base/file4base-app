# File4Base Flutter Client

The Flutter client provides the File4Base desktop application for macOS,
Windows, and Linux, plus the Web Client interface served by Nginx. The client
uses the Go REST API described in the repository's [API reference](../docs/api/API_REFERENCE.md).

## Run the client

Install Flutter for your platform, then run:

```sh
cd client
flutter pub get
flutter run -d macos # Use windows or linux for those desktop targets.
```

The default API endpoint is `http://localhost:8080`. Start the backend and its
database with Docker Compose from the repository root, or configure a reachable
server in the client connection settings.

For repository setup, Docker deployment, desktop packaging, and project
architecture, see the [root README](../README.md) and
[architecture specification](../docs/specs/ARCHITECTURE.md).
