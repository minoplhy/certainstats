# Environment Variables

| Variable | Default | Description |
| :--- | :--- | :--- |
| `PORT` | `8080` | TCP port for the server to listen on. |
| `HOST` | `0.0.0.0` | Host/IP address to bind the server to. |
| `PANEL_URL` | *none* | Fully-qualified URL for the Admin Panel. (Default to `http://HOST:PORT`)|
| `PUBLIC_URL` | *none* | Fully-qualified URL for the Public Dashboards. (Default to `http://HOST:PORT/dashboard/`)|
| `PANEL_PATH` | `/` | **DEPRECATED** Subpath location to mount the Admin Panel. Use `PANEL_URL` instead. |
| `PUBLIC_PATH` | `/dashboard` | **DEPRECATED** Subpath location to mount the Public Dashboards. Use `PUBLIC_URL` instead. |
| `DATA_DIR` | `./data` | Directory for database files (SQLite DB and TSDB). |
| `ALLOWED_ORIGINS` | *none* | Comma-separated list of allowed origins. |
| `DEBUG` | `false` | Enable verbose trace logging. |
| `UPDATE_EVERY` | `60` | Metric sweep frequency (seconds). |
| `BESZEL_EVERY` | `60` | Beszel-agent metrics sweep frequency (syncs to `UPDATE_EVERY` by default). |
