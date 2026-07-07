# dbgate

`dbgate` is a local, test-only HTTP database gateway for workflow automation. It exposes a minimal REST API that lets tools like Hurl, curl, CI setup steps, and test harnesses execute SQL statements against MySQL and query or mutate MongoDB collections without embedding database clients directly into those workflows. It has no authentication and is intentionally meant for trusted local or internal test environments only.

## Configuration reference

`dbgate` reads configuration from `dbgate.yaml` in the working directory and supports environment variable overrides.

### YAML

```yaml
port: 9999

databases:
  - name: platform
    type: mysql
    dsn: root:mysqlroot@tcp(127.0.0.1:3306)/platform
  - name: notifications
    type: mongo
    uri: mongodb://127.0.0.1:27017
    database: notifications
```

### Environment variables

- `DBGATE_PORT`: HTTP port, default `9999`
- `DBGATE_DATABASES`: JSON array of database definitions
- `DBGATE_CONFIG`: optional explicit path to a YAML config file

Example:

```bash
export DBGATE_PORT=9999
export DBGATE_DATABASES='[
  {"name":"platform","type":"mysql","dsn":"root:mysqlroot@tcp(127.0.0.1:3306)/platform"},
  {"name":"notifications","type":"mongo","uri":"mongodb://127.0.0.1:27017","database":"notifications"}
]'
go run ./cmd/dbgate
```

## API reference

### `GET /health`

```bash
curl http://127.0.0.1:9999/health
```

Response:

```json
{"ok":true}
```

### `POST /exec` for MySQL

Multiple SQL statements are enabled automatically through the MySQL driver.

```bash
curl -X POST http://127.0.0.1:9999/exec \
  -H 'Content-Type: application/json' \
  -d '{
    "db": "mysql:platform",
    "sql": "DELETE FROM Foo; INSERT INTO Foo (id, name) VALUES (1, \"alpha\"), (2, \"beta\")"
  }'
```

Response:

```json
{"ok":true}
```

### `POST /query` for MySQL

```bash
curl -X POST http://127.0.0.1:9999/query \
  -H 'Content-Type: application/json' \
  -d '{
    "db": "mysql:platform",
    "sql": "SELECT * FROM Foo WHERE id = 1"
  }'
```

Response:

```json
{
  "rows": [
    {"id": 1, "name": "alpha"}
  ]
}
```

### `POST /exec` for MongoDB

Supported operations: `deleteMany`, `deleteOne`, `insertOne`, `insertMany`, `updateOne`, `updateMany`.

```bash
curl -X POST http://127.0.0.1:9999/exec \
  -H 'Content-Type: application/json' \
  -d '{
    "db": "mongo:notifications",
    "collection": "events",
    "op": "deleteMany",
    "filter": {}
  }'
```

Response:

```json
{"ok":true,"deleted":3}
```

Seed example:

```bash
curl -X POST http://127.0.0.1:9999/exec \
  -H 'Content-Type: application/json' \
  -d '{
    "db": "mongo:notifications",
    "collection": "events",
    "op": "insertMany",
    "documents": [
      {"type": "EMAIL", "status": "queued"},
      {"type": "SMS", "status": "sent"}
    ]
  }'
```

### `POST /query` for MongoDB

```bash
curl -X POST http://127.0.0.1:9999/query \
  -H 'Content-Type: application/json' \
  -d '{
    "db": "mongo:notifications",
    "collection": "events",
    "filter": {"type": "EMAIL"}
  }'
```

Response:

```json
{
  "docs": [
    {"_id":"6870a1f5d0f7c4b04db2de16","type":"EMAIL","status":"queued"}
  ]
}
```

### Error handling

- `400 Bad Request`: invalid JSON, unsupported database target, unsupported Mongo operation, or missing required fields
- `500 Internal Server Error`: database execution/query failures

Database errors are returned in the JSON response body and are never swallowed.

## Docker usage example

Build:

```bash
docker build -t dbgate .
```

Run with a mounted config file:

```bash
docker run --rm \
  -p 9999:9999 \
  -v "$(pwd)/dbgate.yaml.example:/app/dbgate.yaml:ro" \
  dbgate
```

Target Docker Hub image naming for publication:

```text
user/dbgate:<tag>
```

## Kubernetes deploy example

Apply the example manifests into the `test-tools` namespace:

```bash
kubectl create namespace test-tools --dry-run=client -o yaml | kubectl apply -f -
kubectl apply -f deploy/kubernetes/configmap.yaml
kubectl apply -f deploy/kubernetes/deployment.yaml
```

The Deployment mounts `dbgate.yaml` from a ConfigMap at `/app/dbgate.yaml` and still allows environment variables such as `DBGATE_PORT` or `DBGATE_DATABASES` to override values.

## Helmfile deploy example

```bash
cd deploy/helmfile
helmfile apply
```

The release targets namespace `test-tools` and the chart exposes:

- `image.repository`
- `image.tag`
- `image.pullPolicy`
- `config.databases`
- `service.port`

## Hurl usage example

Example `seed-and-assert.hurl`:

```hurl
POST http://127.0.0.1:9999/exec
Content-Type: application/json
{
  "db": "mysql:platform",
  "sql": "DELETE FROM Foo; INSERT INTO Foo (id, name) VALUES (1, \"alpha\"), (2, \"beta\")"
}
HTTP 200
[Asserts]
jsonpath "$.ok" == true

POST http://127.0.0.1:9999/query
Content-Type: application/json
{
  "db": "mysql:platform",
  "sql": "SELECT COUNT(*) AS total FROM Foo"
}
HTTP 200
[Asserts]
jsonpath "$.rows[0].total" == 2
```

Run it with:

```bash
hurl --test seed-and-assert.hurl
```
