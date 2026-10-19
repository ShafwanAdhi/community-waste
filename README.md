# Community Waste Collection API

API pengumpulan sampah komunitas menggunakan Go dan PostgreSQL.

Seluruh 14 endpoint sudah tersedia: household, pickup, payment dengan bukti bayar, dan laporan. Bonus graceful shutdown, unit test bisnis, serta filter tanggal payment sudah diterapkan. Seed contoh dapat dijalankan terpisah. Audit penyerahan dan dokumentasi Postman publik diselesaikan pada checkpoint berikutnya.

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

Response completion berisi pickup yang diperbarui. Gunakan GET payments dengan `household_id` untuk melihat tagihannya. Payment pending memblokir pembuatan pickup baru pada household tersebut. Konfirmasi dan upload bukti bayar tersedia melalui endpoint di bawah.

Daftar payment mengikuti pagination, snapshot, urutan, format UTC, dan aturan error query daftar pickup. Filter status menerima `pending`, `paid`, atau `failed`; filter household_id menerima UUID. Filter tanggal tersedia pada payment_date dengan aturan di bawah.

Kebijakan invoice manual, satu invoice per pickup, pemakaian ulang invoice pending/paid, dan penolakan invoice failed saat completion merupakan asumsi desain tambahan. Completion tidak menunggu waktu pickup_date lewat; perubahan status dilakukan eksplisit oleh pemanggil setelah pekerjaan selesai.

## Konfirmasi pembayaran dan bukti

`PUT /api/payments/{id}/confirm` menerima `multipart/form-data` dengan tepat satu file bernama `proof_file`. Payment harus berstatus `pending`; payment paid/failed menghasilkan `409 payment_not_pending`. Pickup canceled tidak dapat dibayar. Konfirmasi ulang tidak mengganti bukti atau tanggal pembayaran.

```sh
curl -X PUT "http://localhost:8080/api/payments/{id}/confirm" -F "proof_file=@postman/fixtures/proof.pdf"
```

File wajib berisi konten JPEG, PNG, atau PDF, maksimum **5 MiB (5.242.880 byte)**. Server memeriksa signature konten dengan MIME sniffing; nama dan Content-Type yang diberikan klien tidak menentukan tipe file. Pemeriksaan ini tidak mencakup parsing penuh dokumen atau pemindaian malware. File hilang/kosong menghasilkan `422`; form rusak, field tambahan, atau file ganda menghasilkan `400`; tipe tidak didukung menghasilkan `415 unsupported_proof`; ukuran file berlebih menghasilkan `413 proof_too_large`. Seluruh body dibatasi 5 MiB ditambah 64 KiB untuk overhead multipart (`413 body_too_large`).

Response berisi payment dengan status `paid`, `payment_date` UTC, dan `proof_file_url` berupa path relatif `uploads/<UUID>.pdf` (atau `.jpg`/`.png`). Nama file dibuat oleh server. Path menunjuk file lokal dalam volume uploads; API tidak menambahkan endpoint unduh. `UPLOAD_DIR` menentukan direktori fisik bila menjalankan binary sendiri; Compose menggunakan `/app/uploads` yang sudah terhubung ke named volume.

Konfirmasi mengunci household, pickup, lalu payment, sehingga pembatalan dan konfirmasi bersamaan tetap konsisten. File disimpan setelah status diperiksa. Kegagalan penyimpanan mempertahankan status sebelumnya; kegagalan transaksi database yang pasti dibatalkan membersihkan file baru. Jika koneksi putus saat COMMIT sehingga hasilnya tidak pasti, file dipertahankan agar bukti payment yang mungkin sudah paid tidak terhapus. Periksa GET payments sebelum mencoba lagi pada kondisi tersebut. Filesystem dan PostgreSQL tidak berbagi satu transaksi atomik.

Setelah semua payment pending household dibayar atau dibatalkan menjadi failed, household dapat membuat pickup baru.

### Filter tanggal payment

`GET /api/payments` menerima filter opsional `date_from` dan `date_to` pada **payment_date**, bukan created_at. Keduanya RFC3339 dengan `T`/`Z` kapital, tanda titik untuk pecahan detik (maksimum 9 digit), dan offset numerik yang valid; waktu dinormalisasi ke UTC. Batas nanodetik tetap dibandingkan secara tepat terhadap timestamp PostgreSQL yang berpresisi mikrodetik. Batas awal inklusif (`>=`), batas akhir eksklusif (`<`); satu batas saja juga diterima. Bila keduanya diberikan, date_from harus lebih awal dari date_to.

```http
GET /api/payments?status=paid&date_from=2026-10-18T00:00:00Z&date_to=2026-10-19T00:00:00Z
```

Payment tanpa payment_date tidak masuk hasil bila ada batas tanggal. Filter dapat digabung dengan status, household_id, dan pagination. Nilai kosong, berulang, tanggal salah, atau rentang terbalik/sama menghasilkan `400 invalid_filter`. Encode tanda `+` pada offset timezone menjadi `%2B` saat menulis query URL.

## Laporan

| Method | Endpoint | Hasil |
|---|---|---|
| `GET` | `/api/reports/waste-summary` | Jumlah pickup per kombinasi type/status yang memiliki data |
| `GET` | `/api/reports/payment-summary` | Jumlah dan nominal setiap status payment serta total pendapatan |

Laporan mencakup seluruh data, tanpa pagination atau filter. Waste summary diurutkan berdasarkan type lalu status secara alfabetis; database kosong menghasilkan `data: []`.

```json
{"data":[{"type":"organic","status":"pending","count":2},{"type":"plastic","status":"completed","count":1}]}
```

Payment summary selalu menyertakan status `pending`, `paid`, dan `failed` dalam urutan tersebut, termasuk status dengan count dan nominal nol. Pendapatan hanya berasal dari payment `paid`.

```json
{
  "data": {
    "statuses": [
      {"status":"pending","count":2,"amount":"150000.00"},
      {"status":"paid","count":2,"amount":"100000.00"},
      {"status":"failed","count":2,"amount":"150000.00"}
    ],
    "total_revenue":"100000.00"
  }
}
```

Contoh angka payment di atas adalah hasil seed pada database kosong. Data tambahan ikut dihitung. Count dan nominal tiap laporan berasal dari satu query; pendapatan memakai hasil agregasi paid yang sama. SQL menjumlahkan NUMERIC secara tepat, lalu mengonversi hasil ke int64 dalam satuan 1/100 rupiah. Total dapat melampaui batas nominal satu payment; jika melampaui kapasitas int64, laporan gagal dengan `500` alih-alih menghasilkan nominal yang terpotong atau overflow.

## Konfigurasi

Default Compose cukup untuk lingkungan lokal. Salin `.env.example` menjadi `.env` untuk mengubahnya.

| Variable | Default | Kegunaan |
|---|---|---|
| `APP_PORT` | `8080` | Port host pada Compose; port internal container tetap 8080 |
| `POSTGRES_USER` | `community` | User PostgreSQL lokal |
| `POSTGRES_PASSWORD` | `community_local` | Password PostgreSQL lokal |
| `POSTGRES_DB` | `community_waste` | Nama database aplikasi |
| `DATABASE_URL` | Lihat `.env.example` | URL PostgreSQL aplikasi dan migration; hostname `db` pada Compose |
| `UPLOAD_DIR` | `uploads` | Direktori bukti untuk binary lokal; Compose menetapkan `/app/uploads` |
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

## Seed contoh

Setelah aplikasi dan migration siap, jalankan:

```sh
docker compose exec app /app/community-waste seed
```

Alternatif tanpa proses server yang sedang berjalan:

```sh
docker compose run --rm app seed
```

Seed tidak berjalan otomatis saat startup. Perintah hanya mengisi snapshot contoh dalam satu transaksi, lalu keluar tanpa membuka HTTP listener. Output log menunjukkan jumlah record baru; pada database kosong hasilnya `households=4`, `pickups=16`, `payments=6`. Pengulangan tanpa perubahan menghasilkan nol untuk ketiganya.

| Data | Isi |
|---|---|
| Household | Sari, Bima, Citra, Damar dengan alamat contoh |
| Pickup | Seluruh 16 kombinasi empat type dan empat status; electronic pending/canceled false, scheduled/completed true |
| Payment | Dua pending, dua paid, dua failed; tarif sesuai tipe pickup |
| Bukti | Dua PDF contoh untuk payment paid, disimpan melalui adapter uploads dengan nama UUID |
| Tanggal | Snapshot tetap 15–16 Oktober 2026; pickup scheduled bertanggal 20 Oktober 2026 |

UUID contoh tetap memakai prefix `10000000` untuk household, `20000000` untuk pickup, dan `30000000` untuk payment; nomor akhir dimulai dari `000000000101`. Seed menyisipkan data yang belum ada dan mempertahankan status, tanggal, maupun path bukti yang sudah berubah. Invoice yang sudah dimiliki pickup dipakai sebagaimana adanya bila ownership dan nominal cocok; record lain tidak dihapus atau diubah. Benturan identitas tetap yang tidak cocok menggagalkan seluruh transaksi.

Dua perintah seed bersamaan diserialisasi dengan advisory lock, lalu mengambil lock household sebelum pickup/payment seperti mutasi API. File baru dibersihkan bila penulisan atau COMMIT ditolak secara pasti; hasil COMMIT yang tidak pasti mempertahankan bukti, mengikuti kebijakan konfirmasi. Seed tidak bertujuan memperbaiki korupsi manual atau file yang dihapus di luar aplikasi.

Pada snapshot awal, total pending `150000.00`, paid `100000.00`, failed `150000.00`; pendapatan `100000.00`. Household Bima dan Damar memiliki invoice pending sehingga pickup baru ditolak sampai tagihan dikonfirmasi. Sari dan Citra tidak mempunyai tagihan pending. Pickup terbuka pada snapshot merupakan permintaan yang dibuat sebelum invoice pending muncul.

## Pengujian

```sh
docker compose --profile test run --build --rm test
docker compose --profile test run --rm test go vet ./...
docker compose --profile test run --rm test gofmt -l cmd internal migrations
```

Perintah pertama menjalankan seluruh test dengan race detector. Integration test menggunakan PostgreSQL terpisah di tmpfs dan membuat schema unik per pengujian. Migration up/down tidak dijalankan pada database aplikasi. Output `gofmt -l` harus kosong.

Pengujian laporan mencakup data kosong, kombinasi type/status, ketiga status payment, paid-only revenue, nominal desimal, serta agregat yang melampaui nominal maksimum satu payment. Pengujian seed mencakup snapshot lengkap, pengulangan tanpa duplikasi bukti, perubahan existing yang dipertahankan, benturan UUID, rollback database/storage/COMMIT, dan antrean seed bersamaan. Pengujian konfirmasi mencakup signature JPEG/PNG/PDF, batas tepat 5 MiB, multipart salah/hilang/ganda, kegagalan storage/database, cleanup file, penolakan konfirmasi ulang, konkurensi cancel/confirm/pickup baru, serta batas tanggal inklusif/eksklusif dan NULL. Pengujian billing mencakup parsing uang, empat tarif, invoice manual/otomatis, reuse invoice, rollback pada kegagalan INSERT payment maupun UPDATE pickup, dan antrean transaksi bersamaan yang diverifikasi melalui lock PostgreSQL. Pengujian pickup mencakup empat tipe sampah, electronic safety check, tanggal mendatang, transisi status, filter/pagination, payment pending/paid, transaksi rollback, dan request bersamaan. Pengujian lain mencakup validasi konfigurasi, response health dan routing error, penyelesaian request saat shutdown, migration up/down, CRUD household, validasi body/UUID/query, pagination, penanganan kegagalan database, serta constraint relasi dan penghapusan. Pengujian schema juga mencakup status, tipe sampah, safety check electronic, nominal, dan duplikasi payment.

Collection Postman tersedia di `postman/community-waste.postman_collection.json`. Import collection lalu jalankan seluruh collection terhadap aplikasi yang aktif. Ubah variable `base_url` bila memakai port berbeda. Folder Households membuat record sendiri, menyimpan ID ke variable `household_id`, lalu menghapus record tersebut di akhir alur.

Collection memakai fixture `postman/fixtures/proof.pdf` dan file teks untuk kasus tipe salah. Pada Postman Desktop, atur working directory ke root repository agar path relatif file dapat diakses. Jalankan CLI dari root repository.

Jika Postman CLI tersedia:

```sh
postman collection run postman/community-waste.postman_collection.json --no-report-events
```

Collection mencakup health, CRUD household, alur pickup, pagination/filter, safety check, konflik status, dan input tidak valid. Folder Pickups membuat household khusus dan membatalkan pickup miliknya pada akhir alur. Household dan riwayat pickup dari folder ini tetap tersimpan karena API tidak menyediakan penghapusan pickup. Setiap run memakai fixture baru dan tidak mengubah data yang sudah ada. Folder Billing membuat household dan pickup khusus untuk invoice manual/otomatis, reuse, penolakan pickup baru, serta pembatalan invoice. Riwayat completed/canceled dan invoice dari folder ini tetap tersimpan; invoice pending memblokir pickup baru pada household fixture tersebut. Folder Confirmation menjalankan upload bukti, filter tanggal, penolakan konfirmasi ulang, dan pembuatan pickup baru setelah dibayar. Bukti dan riwayat fixture tetap tersimpan dalam volume. Folder Reporting memeriksa perubahan agregat dari pickup hingga konfirmasi memakai baseline data yang sudah ada, sehingga assertion tetap berlaku pada database berisi seed maupun data lain. Dokumentasi Postman publik dan audit akhir ditambahkan pada checkpoint penyerahan.

## Struktur

```text
cmd/api/            Entrypoint server, migration, dan healthcheck CLI
internal/config/    Pembacaan dan validasi environment
internal/database/  Connection pool dan eksekusi migration
internal/domain/    Model, pagination, dan error aplikasi
internal/handler/   Routing HTTP dan response JSON
internal/service/   Validasi input dan aturan aplikasi
internal/repository/postgres/  Query dan pemetaan error PostgreSQL
internal/seed/      Snapshot contoh transaksional dan bukti PDF yang di-embed
internal/storage/   Penyimpanan dan cleanup bukti lokal
internal/server/    Lifecycle server dan graceful shutdown
internal/testutil/  Fixture database terisolasi untuk pengujian
migrations/         SQL up/down yang di-embed ke binary
postman/            Collection request dan assertion API
```

Handler menangani protokol HTTP dan pemetaan error. Service memvalidasi input dan bergantung pada interface repository. Implementasi PostgreSQL menjalankan SQL berparameter serta menerjemahkan error relasi menjadi error aplikasi. Entrypoint menghubungkan ketiganya melalui constructor biasa.
