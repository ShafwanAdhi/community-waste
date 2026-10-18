# Community Waste Collection API

API pengumpulan sampah komunitas menggunakan Go dan PostgreSQL.

Checkpoint saat ini menyediakan Household API, konfigurasi, health endpoint, migration tiga tabel, dan lingkungan Docker. Endpoint pickup, payment, reporting, serta seed belum tersedia pada checkpoint ini.

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

## Household API

| Method | Endpoint | Hasil |
|---|---|---|
| `POST` | `/api/households` | Membuat household; HTTP 201 dan header `Location` |
| `GET` | `/api/households?page=1&limit=20` | Daftar household dengan pagination |
| `GET` | `/api/households/{id}` | Detail household |
| `DELETE` | `/api/households/{id}` | Menghapus household tanpa relasi; HTTP 204 tanpa body |

Contoh pembuatan household:

```http
POST /api/households
Content-Type: application/json

{"owner_name":"Ayu","address":"Jl. Merdeka 1"}
```

```json
{
  "data": {
    "id": "10000000-0000-4000-8000-000000000001",
    "owner_name": "Ayu",
    "address": "Jl. Merdeka 1",
    "created_at": "2026-01-01T00:00:00Z",
    "updated_at": "2026-01-01T00:00:00Z"
  }
}
```

UUID dan timestamp pada contoh bersifat ilustratif. Server membuat UUID; timestamp response memakai UTC. `owner_name` dan `address` wajib berupa string yang tidak kosong setelah spasi di awal/akhir dibuang. Karakter NUL tidak diterima karena tidak dapat disimpan dalam PostgreSQL TEXT; pelanggaran menghasilkan validasi field `422`.

Body POST harus satu objek JSON, hanya dengan field `owner_name` dan `address`, maksimum 64 KiB. Field tambahan, JSON rusak, body kosong, atau lebih dari satu objek menghasilkan `400`. Content-Type selain `application/json` menghasilkan `415`; body terlalu besar menghasilkan `413`.

Validasi field menghasilkan `422`:

```json
{
  "error": {
    "code": "validation_failed",
    "message": "Input validation failed",
    "fields": {"owner_name": "Owner name is required"}
  }
}
```

Response daftar menambahkan metadata:

```json
{"data":[],"meta":{"page":1,"limit":20,"total":0}}
```

Default `page=1` dan `limit=20`; keduanya bilangan bulat positif, dengan limit maksimum 100. Parameter kosong/berulang atau offset yang melampaui int64 menghasilkan `400`. Urutan daftar adalah `created_at DESC, id DESC`. Jumlah total dan isi halaman dibaca dari snapshot yang sama. Halaman di luar jangkauan tetap mengembalikan array kosong dan total yang benar.

ID pada URL harus UUID berformat `8-4-4-4-12`; format salah menghasilkan `400`, sedangkan UUID yang tidak ditemukan menghasilkan `404`. Penghapusan household yang memiliki pickup atau payment menghasilkan `409` dengan kode `household_in_use`. Foreign key database menjaga aturan ini. Kesalahan internal menghasilkan JSON `500` tanpa detail SQL atau koneksi.

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

Pengujian mencakup validasi konfigurasi, response health dan routing error, penyelesaian request saat shutdown, migration up/down, CRUD household, validasi body/UUID/query, pagination, penanganan kegagalan database, serta constraint relasi dan penghapusan. Pengujian schema juga mencakup status, tipe sampah, safety check electronic, nominal, dan duplikasi payment.

Collection Postman tersedia di `postman/community-waste.postman_collection.json`. Import collection lalu jalankan seluruh collection terhadap aplikasi yang aktif. Ubah variable `base_url` bila memakai port berbeda. Folder Households membuat record sendiri, menyimpan ID ke variable `household_id`, lalu menghapus record tersebut di akhir alur.

Jika Postman CLI tersedia:

```sh
postman collection run postman/community-waste.postman_collection.json --no-report-events
```

Collection mencakup health, CRUD household, pagination, dan kasus input tidak valid. Request pickup/payment/report serta dokumentasi Postman publik ditambahkan pada milestone berikutnya. Kasus penghapusan household berelasi sudah diuji melalui HTTP dan PostgreSQL pada integration test; collection akan mencakupnya setelah endpoint pickup tersedia.

## Struktur

```text
cmd/api/            Entrypoint server, migration, dan healthcheck CLI
internal/config/    Pembacaan dan validasi environment
internal/database/  Connection pool dan eksekusi migration
internal/domain/    Model, pagination, dan error aplikasi
internal/handler/   Routing HTTP dan response JSON
internal/service/   Validasi input dan aturan aplikasi
internal/repository/postgres/  Query dan pemetaan error PostgreSQL
internal/server/    Lifecycle server dan graceful shutdown
internal/testutil/  Fixture database terisolasi untuk pengujian
migrations/         SQL up/down yang di-embed ke binary
postman/            Collection request dan assertion API
```

Handler menangani protokol HTTP dan pemetaan error. Service memvalidasi input dan bergantung pada interface repository. Implementasi PostgreSQL menjalankan SQL berparameter serta menerjemahkan error relasi menjadi error aplikasi. Entrypoint menghubungkan ketiganya melalui constructor biasa.
