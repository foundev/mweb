# mweb

My web server written in Go with no third party dependencies.

## Run

```bash
go run .
```

The server listens on `:8080` by default. Set `PORT` to override.

### Configuration file

Use `-config` to point to a JSON configuration file. When present, the server uses the port from the file (if set) and routes responses based on the request `Host` header.

```bash
go run . -config ./config.json
```

Example `config.json`:

```json
{
  "port": 9090,
  "default_host": "example.com",
  "hosts": [
    {
      "name": "example.com",
      "directory": "./public"
    },
    {
      "name": "admin.example.com",
      "directory": "./admin"
    }
  ]
}
```

## Endpoints

- `GET /` serves files from the configured `directory` for the matched host.
- When a config file is provided, each `host` entry must set a `directory`.
