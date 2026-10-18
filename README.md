# Community Waste Collection API

API pengumpulan sampah komunitas menggunakan Go dan PostgreSQL.

Checkpoint saat ini menyediakan fondasi aplikasi: konfigurasi, health endpoint, migration tiga tabel, dan lingkungan Docker. Endpoint household, pickup, payment, reporting, serta seed belum tersedia pada checkpoint ini.

## Menjalankan aplikasi

Prasyarat: Docker Engine aktif dan Docker Compose v2. Go lokal tidak diperlukan.

```sh
docker compose up --build -d
docker compose ps -a
docker compose exec app /app/community-waste healthcheck
```

Aplikasi tersedia di `http://localhost:8080`. PostgreSQL menunggu sampai sehat, migration dijalankan sampai selesai, lalu aplikasi mulai menerima request. Database tidak membuka port ke host.

```http
GET /healthz
```

```json
{"data":{"status":"ok"}}
```

Health endpoint melakukan ping database. Jika database tidak tersedia, endpoint mengembalikan HTTP `503`:

```json
{"error":{"code":"service_unavailable","message":"Database is unavailable"}}
```

Path yang tidak dikenal menghasilkan JSON `404`. Method selain GET/HEAD pada health endpoint menghasilkan JSON `405` beserta header `Allow`.

```sh
docker compose logs -f app migrate
docker compose stop
docker compose up -d
```

Database dan direktori uploads memakai named volume sehingga tetap tersedia ketika container dibuat ulang. Proses aplikasi berjalan sebagai user non-root. SIGINT/SIGTERM menghentikan penerimaan request baru dan memberi waktu bagi request berjalan untuk selesai.

## Konfigurasi

Default Compose cukup untuk lingkungan lokal. Salin `.env.example` menjadi `.env` untuk mengubahnya.

| Variable | Default | Kegunaan |
|---|---|---|
| `APP_PORT` | `8080` | Port host pada Compose; port internal container tetap 8080 |
| `POSTGRES_USER` | `community` | User PostgreSQL lokal |
| `POSTGRES_PASSWORD` | `community_local` | Password PostgreSQL lokal |
| `POSTGRES_DB` | `community_waste` | Nama database aplikasi |
| `DATABASE_URL` | Lihat `.env.example` | URL PostgreSQL aplikasi dan migration; hostname `db` pada Compose |
| `SHUTDOWN_TIMEOUT` | `10s` | Batas waktu menyelesaikan request ketika shutdown |
| `TEST_DATABASE_URL` | Diatur service `test` | Database terpisah untuk integration test |

Kredensial default ditujukan untuk pengembangan lokal. Jika `POSTGRES_*` diubah, sesuaikan `DATABASE_URL`. Perubahan kredensial tidak mengubah user/database pada volume PostgreSQL yang sudah diinisialisasi.

Compose memberi proses waktu 15 detik untuk berhenti. Sesuaikan `stop_grace_period` jika menaikkan `SHUTDOWN_TIMEOUT` di atas 10 detik.

Versi utama: Go 1.27.2, PostgreSQL 18.6, pgx v5.11.0, golang-migrate v4.20.1.

## Migration

File SQL up/down disertakan dalam binary. Menjalankan migration up kembali tidak mengubah schema yang sudah terkini.

```sh
docker compose run --rm migrate migrate up
```

Untuk membatalkan satu migration terakhir:

```sh
docker compose stop app
docker compose run --rm migrate migrate down
```

Migration down awal menghapus ketiga tabel beserta datanya. Jalankan hanya pada database yang datanya boleh dihapus. Setelah migration up kembali, jalankan `docker compose up -d`.

Schema menggunakan UUID, timestamp dengan timezone, dan constraint status/type. Satu pickup memiliki paling banyak satu payment. Foreign key gabungan memastikan household pada payment sama dengan pemilik pickup. Household yang masih memiliki relasi tidak dapat dihapus. Nominal memakai `NUMERIC(12,2)` dan harus positif.

Seed akan ditambahkan bersama data contoh pada milestone berikutnya; belum ada perintah seed pada checkpoint ini.

## Pengujian

```sh
docker compose --profile test run --build --rm test
docker compose --profile test run --rm test go vet ./...
docker compose --profile test run --rm test gofmt -l cmd internal migrations
```

Perintah pertama menjalankan seluruh test dengan race detector. Integration test menggunakan PostgreSQL terpisah di tmpfs dan membuat schema unik per pengujian. Migration up/down tidak dijalankan pada database aplikasi. Output `gofmt -l` harus kosong.

Pengujian mencakup validasi konfigurasi, response health dan routing error, penyelesaian request saat shutdown, migration up/down, serta constraint database untuk relasi, status, tipe sampah, safety check electronic, nominal, dan duplikasi payment.

Collection Postman tersedia di `postman/community-waste.postman_collection.json`. Import collection lalu jalankan folder Foundation terhadap aplikasi yang aktif. Ubah variable `base_url` bila memakai port berbeda.

Jika Postman CLI tersedia:

```sh
postman collection run postman/community-waste.postman_collection.json --no-report-events
```

Collection saat ini berisi health endpoint dan pemeriksaan error HTTP. Request bisnis dan dokumentasi Postman publik ditambahkan ketika fiturnya tersedia.

## Struktur

```text
cmd/api/            Entrypoint server, migration, dan healthcheck CLI
internal/config/    Pembacaan dan validasi environment
internal/database/  Connection pool dan eksekusi migration
internal/handler/   Routing HTTP dan response JSON
internal/server/    Lifecycle server dan graceful shutdown
migrations/         SQL up/down yang di-embed ke binary
postman/            Collection request dan assertion API
```

Aturan bisnis akan ditempatkan pada service; SQL untuk fitur bisnis berada pada repository. Package tersebut ditambahkan bersama fitur yang menggunakannya.
