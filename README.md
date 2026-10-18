# Community Waste Collection API

API pengumpulan sampah komunitas menggunakan Go dan PostgreSQL.

Checkpoint saat ini menyediakan Household API, alur pickup hingga completion, serta pembuatan dan daftar payment. Konfirmasi bukti bayar, filter tanggal payment, reporting, serta seed belum tersedia pada checkpoint ini.

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

## Pickup API

| Method | Endpoint | Hasil |
|---|---|---|
| `POST` | `/api/pickups` | Membuat pickup dengan status `pending`; HTTP 201 |
| `GET` | `/api/pickups?page=1&limit=20&status=pending&household_id={id}` | Daftar dengan pagination dan filter opsional |
| `PUT` | `/api/pickups/{id}/schedule` | Mengubah `pending` menjadi `scheduled` |
| `PUT` | `/api/pickups/{id}/cancel` | Membatalkan pickup `pending` atau `scheduled` |
| `PUT` | `/api/pickups/{id}/complete` | Menyelesaikan pickup `scheduled` dan memastikan invoice tersedia |

Contoh permintaan electronic:

```http
POST /api/pickups
Content-Type: application/json

{"household_id":"10000000-0000-4000-8000-000000000001","type":"electronic","safety_check":false}
```

`type` harus `organic`, `plastic`, `paper`, atau `electronic`. Household harus ada. Electronic wajib menyertakan boolean `safety_check`; `false` diterima saat dibuat, sedangkan field hilang atau `null` menghasilkan `422`. Pickup baru ditolak dengan `409 pending_payment` jika household mempunyai payment berstatus `pending`.

```http
PUT /api/pickups/{id}/schedule
Content-Type: application/json

{"pickup_date":"2026-10-25T08:00:00+07:00","safety_check":true}
```

`pickup_date` wajib RFC3339 dan berada di masa mendatang saat transaksi memperoleh lock. `safety_check` opsional saat schedule; field hilang atau `null` mempertahankan nilai tersimpan. Electronic hanya dapat dijadwalkan bila nilai akhir safety check `true`; jika belum, hasilnya `409 safety_check_required`. Penjadwalan ulang ditolak dengan `409 invalid_transition`.

Response mutasi memakai `data` berisi `id`, `household_id`, `type`, `status`, `pickup_date`, `safety_check`, `created_at`, dan `updated_at`. Waktu dinormalisasi ke UTC; tanggal yang belum ditentukan dan safety check yang tidak diberikan bernilai JSON `null`.

Pembatalan tidak memerlukan body. Status `completed` dan `canceled` bersifat terminal. Payment pending terkait berubah menjadi `failed` dalam transaksi pembatalan yang sama; payment `paid` menghalangi pembatalan dengan `409 payment_already_paid`. Pembatalan mempertahankan tanggal dan hasil safety check sebagai riwayat.

Daftar pickup memakai aturan pagination dan urutan yang sama dengan household. Filter `status` menerima `pending`, `scheduled`, `completed`, atau `canceled`; `household_id` menerima UUID. Filter kosong, berulang, atau tidak valid menghasilkan `400 invalid_filter`. Query dengan encoding URL rusak ditolak lebih awal oleh parser bersama sebagai `400 invalid_pagination`, termasuk bila encoding rusak berada pada filter. Kombinasi filter tanpa hasil mengembalikan `data: []`.

Aturan body JSON, batas 64 KiB, dan error `400`/`413`/`415` berlaku pada create dan schedule. UUID URL yang salah menghasilkan `400`; resource yang hilang menghasilkan `404`. Input bisnis tidak valid menghasilkan `422`; konflik status menghasilkan `409`.

### Asumsi desain

Pembatalan boleh dilakukan pada status pending/scheduled, sedangkan completed/canceled tidak dapat diubah lagi. Payment paid melarang pembatalan; payment pending dibatalkan menjadi failed. Household dengan riwayat tetap tidak dapat dihapus. Kebijakan tambahan ini menjelaskan perilaku yang tidak dirinci oleh soal.

Mutasi pickup mengunci household lebih dahulu, lalu pickup dan payment bila diperlukan. Service memegang aturan bisnis dan batas transaksi; repository menyediakan operasi SQL dalam transaksi tersebut. Pengujian PostgreSQL memeriksa dua penjadwalan bersamaan, penjadwalan bersamaan dengan pembatalan, serta rollback jika pembaruan pickup gagal setelah invoice diubah. Mutasi payment memakai urutan lock yang sama agar pemeriksaan payment pending tetap berlaku ketika request berjalan bersamaan.

## Payment dan completion

| Method | Endpoint | Hasil |
|---|---|---|
| `POST` | `/api/payments` | Membuat invoice manual `pending`; HTTP 201 |
| `GET` | `/api/payments?page=1&limit=20&status=pending&household_id={id}` | Daftar payment dengan filter opsional |
| `PUT` | `/api/pickups/{id}/complete` | Mengubah pickup `scheduled` menjadi `completed`; HTTP 200 |

```http
POST /api/payments
Content-Type: application/json

{"household_id":"10000000-0000-4000-8000-000000000001","waste_id":"20000000-0000-4000-8000-000000000001","amount":"50000.00"}
```

`waste_id` adalah ID pickup. Invoice manual hanya dapat dibuat untuk pickup `pending` atau `scheduled`. Household harus memiliki pickup tersebut; ketidakcocokan menghasilkan `409 household_mismatch`. Pickup yang tidak ditemukan menghasilkan `404`. Satu pickup mempunyai paling banyak satu payment; percobaan duplikasi pada pickup aktif menghasilkan `409 payment_exists`, termasuk jika invoice sebelumnya sudah failed.

| Type | Tarif |
|---|---|
| organic, plastic, paper | `"50000.00"` |
| electronic | `"100000.00"` |

`amount` dikirim sebagai string desimal positif, maksimum `9999999999.99`. String bilangan bulat serta satu/dua digit pecahan diterima; response selalu mempunyai dua digit pecahan. Angka JSON menghasilkan `400`; nominal nol, negatif, notasi eksponen, lebih dari dua digit pecahan, atau tarif yang tidak sesuai menghasilkan `422` dengan detail field `amount`. Go menggunakan int64 dalam satuan 1/100 rupiah; PostgreSQL memakai NUMERIC(12,2), tanpa perhitungan float.

```json
{
  "data": {
    "id": "30000000-0000-4000-8000-000000000001",
    "household_id": "10000000-0000-4000-8000-000000000001",
    "waste_id": "20000000-0000-4000-8000-000000000001",
    "amount": "50000.00",
    "payment_date": null,
    "status": "pending",
    "proof_file_url": null,
    "created_at": "2026-10-18T10:00:00Z",
    "updated_at": "2026-10-18T10:00:00Z"
  }
}
```

Completion tidak memerlukan body dan hanya berlaku dari `scheduled`. Server membuat invoice pending sesuai tarif bila belum ada. Invoice manual dengan nominal tepat dan status pending/paid dipakai ulang dengan ID yang sama. Invoice failed atau nominal yang tidak konsisten menghasilkan `409 payment_conflict`. Perubahan status pickup dan pembuatan invoice berada dalam satu transaksi; kegagalan salah satu penulisan membatalkan seluruh perubahan. Completion ulang menghasilkan `409 invalid_transition`.

Response completion berisi pickup yang diperbarui. Gunakan GET payments dengan `household_id` untuk melihat tagihannya. Payment pending memblokir pembuatan pickup baru pada household tersebut. Konfirmasi dan upload bukti bayar ditambahkan pada milestone berikutnya.

Daftar payment mengikuti pagination, snapshot, urutan, format UTC, dan aturan error query daftar pickup. Filter status menerima `pending`, `paid`, atau `failed`; filter household_id menerima UUID. Endpoint ini belum menerapkan filter tanggal pada checkpoint ini.

Kebijakan invoice manual, satu invoice per pickup, pemakaian ulang invoice pending/paid, dan penolakan invoice failed saat completion merupakan asumsi desain tambahan. Completion tidak menunggu waktu pickup_date lewat; perubahan status dilakukan eksplisit oleh pemanggil setelah pekerjaan selesai.

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

Pengujian billing mencakup parsing uang, empat tarif, invoice manual/otomatis, reuse invoice, rollback pada kegagalan INSERT payment maupun UPDATE pickup, dan antrean transaksi bersamaan yang diverifikasi melalui lock PostgreSQL. Pengujian pickup mencakup empat tipe sampah, electronic safety check, tanggal mendatang, transisi status, filter/pagination, payment pending/paid, transaksi rollback, dan request bersamaan. Pengujian lain mencakup validasi konfigurasi, response health dan routing error, penyelesaian request saat shutdown, migration up/down, CRUD household, validasi body/UUID/query, pagination, penanganan kegagalan database, serta constraint relasi dan penghapusan. Pengujian schema juga mencakup status, tipe sampah, safety check electronic, nominal, dan duplikasi payment.

Collection Postman tersedia di `postman/community-waste.postman_collection.json`. Import collection lalu jalankan seluruh collection terhadap aplikasi yang aktif. Ubah variable `base_url` bila memakai port berbeda. Folder Households membuat record sendiri, menyimpan ID ke variable `household_id`, lalu menghapus record tersebut di akhir alur.

Jika Postman CLI tersedia:

```sh
postman collection run postman/community-waste.postman_collection.json --no-report-events
```

Collection mencakup health, CRUD household, alur pickup, pagination/filter, safety check, konflik status, dan input tidak valid. Folder Pickups membuat household khusus dan membatalkan pickup miliknya pada akhir alur. Household dan riwayat pickup dari folder ini tetap tersimpan karena API tidak menyediakan penghapusan pickup. Setiap run memakai fixture baru dan tidak mengubah data yang sudah ada. Folder Billing membuat household dan pickup khusus untuk invoice manual/otomatis, reuse, penolakan pickup baru, serta pembatalan invoice. Riwayat completed/canceled dan invoice dari folder ini tetap tersimpan; invoice pending memblokir pickup baru pada household fixture tersebut. Konfirmasi, filter tanggal, reporting, dan dokumentasi Postman publik ditambahkan pada milestone berikutnya.

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
