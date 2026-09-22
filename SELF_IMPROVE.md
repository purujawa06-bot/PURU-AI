# Rencana Peningkatan PURU-AI

## Analisis Struktur
- `internal/ai/agent.go`: Inti logika pemrosesan pesan, manajemen konteks, dan integrasi LLM.
- `internal/ai/tools.go`: Definisi dan implementasi alat (tools) yang dapat dipanggil oleh AI.
- `AGENTS.md`: Dokumentasi filosofi dan cara kerja asisten.

## Rencana Peningkatan
1. **Validasi Input Tool**: Menambahkan pengecekan tipe data yang lebih ketat sebelum eksekusi shell/file. (Selesai)
2. **Logging Error Detail**: Memperbaiki pesan error agar lebih informatif bagi AI saat terjadi kegagalan tool. (Selesai)
3. **Optimasi Konteks**: Mengurangi token overhead dengan ringkasan histori yang lebih cerdas. (Selesai)
4. **Resilience & Safety**: Pengecekan keberadaan file dan folder yang lebih proaktif.

## Implementasi Terpilih (Ronde 1)
Meningkatkan pesan error pada tool `read_file` agar memberikan informasi lebih jelas saat file tidak ditemukan atau akses ditolak. (Selesai)

## Implementasi Terpilih (Ronde 2)
Meningkatkan validasi input pada tool `exec` dengan memastikan `command` tidak kosong dan membersihkan whitespace (trim) pada argumen berbasis string. (Selesai)

## Implementasi Terpilih (Ronde 3)
Meningkatkan detail pesan error pada tool `read_file`, `write_file`, dan `exec` untuk membantu AI melakukan koreksi mandiri lebih cepat. (Selesai)

## Implementasi Terpilih (Ronde 4)
Optimasi Konteks dengan membatasi panjang output tool yang dikirim ke summarizer (max 1000 karakter per tool result) dalam `internal/memory/memory.go`. (Selesai)

## Implementasi Terpilih (Ronde 5)
Penyempurnaan Optimasi Konteks dengan membatasi output `list_dir` maksimal 1000 entri dalam `internal/ai/fs.go` dan sinkronisasi dokumentasi di `AGENTS.md`. (Selesai)

## Implementasi Terpilih (Ronde 6)
Meningkatkan informasi pada tool `list_dir` dengan menambahkan ukuran file yang human-readable. (Selesai)

## Implementasi Terpilih (Ronde 7)
Menambahkan validasi ukuran file sebelum pembacaan penuh pada `telegram_sendfile` menggunakan `os.Stat`. (Selesai)

## Implementasi Terpilih (Ronde 8)
Peningkatan pesan pemangkasan (truncation) pada tool `exec` agar menyertakan total ukuran output asli via `formatSize`. (Selesai)

## Implementasi Terpilih (Ronde 9)
Perbaikan error `telegram_sendfile` agar menolak direktori dan melaporkan ukuran file human-readable via `formatSize`. (Selesai)

## Implementasi Terpilih (Ronde 10)
Menambahkan tool `get_env` untuk memberikan informasi lingkungan (OS, Arch, Go version, dan status Workspace) secara langsung kepada AI. (Selesai)

## Implementasi Terpilih (Ronde 11)
Meningkatkan tool `exec` untuk mendukung aksi background (`background=true` return sessionId) + `list`/`poll`/`read`/`kill` agar AI bisa memantau proses asinkron. Catatan: fitur ini BESAR (sesi global, goroutine watcher, polling loop) dan ronde 11 timeout sebelum selesai — verifikasi manual WAJIB sebelum dianggap selesai.

## Implementasi Terpilih (Ronde 12)
Meningkatkan `get_env` dengan informasi penggunaan memori (RAM) saat ini untuk memberikan kesadaran resource pada asisten. (Selesai)

## Implementasi Terpilih (Ronde 13)
Optimasi tool `list_dir` dengan pengurutan (Direktori dahulu) dan peningkatan pesan error pada `edit_file` untuk membantu diagnosis kegagalan edit. (Selesai)

## Implementasi Terpilih (Ronde 14)
Perbaikan race-condition pada sesi `exec` di `internal/ai/exec.go`: `cappedWriter` dibuat thread-safe (mutex) agar `Write` proses dan `String` via `read` tidak balapan, status sesi dibaca/tulis via `snapshot()`/`finish()` di bawah lock sesi, map sesi memakai `RWMutex` + `lookupSession`. `Success` blocking kini mensyaratkan proses benar selesai (`!running`). Teruji: `gofmt` bersih, `go vet ./internal/ai/` bersih, `go test ./internal/ai/ -count=1` OK. (Selesai)
## Implementasi Terpilih (Ronde 15)
Batasi memori sesi `exec` di `internal/ai/exec.go`: map `sessions` di-cap `maxExecSessions=20` via `pruneSessionsLocked()` (evict finished terlama by StartTime, running tak pernah di-evict) + `listSessions()` diurutkan by sessionId agar deterministik. Bug yang diperbaiki: sesi blocking+background tak pernah dihapus sehingga buffer 20k/sesi menumpuk tanpa batas. Teruji: `gofmt` bersih, `go vet ./internal/ai/` bersih, `go test ./internal/ai/ -count=1` OK + test baru `TestPruneSessionsCapsFinished` di `exec_prune_test.go`. Dok: AGENTS.md + README.md sinkron.

## Slot Ronde 16
Cari bug/inkonsistensi berikutnya (kandidat: `resolveWorkdir` return workspace tanpa cek direktori ada; `readExec`/`pollExec` tanpa info timed_out/memory di read; `get_env` memory_mb uint64 bisa overflow int di 32-bit).

## Implementasi Terpilih (Ronde 16)
Samakan `readExec` dengan `pollExec` di `internal/ai/exec.go`: `read` kini kembalikan `status` + `timed_out` + `memory_limited` selain `output`/`running`, agar AI tahu kenapa proses berhenti (timeout/OOM) tanpa panggil `poll` kedua. Teruji: `gofmt` bersih, `go vet ./internal/ai/` bersih, `go test ./internal/ai/ -count=1` OK + test baru `TestReadExecCarriesStatusFlags` di `exec_prune_test.go`. Dok: AGENTS.md + README.md sinkron.

## Slot Ronde 17
Cari bug/inkonsistensi berikutnya (kandidat sisa: `resolveWorkdir` return workspace tanpa cek direktori ada — `cmd.Dir` tak ada bikin `cmd.Start` gagal dgn pesan generik; `get_env` memory_mb uint64 bisa overflow int di 32-bit; `killExec` tak panggil `sess.finish()` sehingga sesi killed tetap `running` sampai watcher jalan).

## Implementasi Terpilih (Ronde 17)
Perbaikan `killExec` di `internal/ai/exec.go`: `kill` kini panggil `sess.finish()` langsung (nil-safe bila `Cmd` nil di test) agar sesi killed segera `finished` — sebelumnya tetap `running` sampai watcher `cmd.Wait()` jalan sehingga `poll`/`read`/`list` menyesatkan. Teruji: `gofmt` bersih, `go vet ./internal/ai/` bersih, `go test ./internal/ai/ -count=1` OK + test baru `TestKillExecMarksFinishedImmediately` di `exec_prune_test.go`. Dok: AGENTS.md + README.md sinkron.

## Slot Ronde 18
Cari bug/inkonsistensi berikutnya (kandidat sisa: `resolveWorkdir` return workspace tanpa cek direktori ada — `cmd.Dir` tak ada bikin `cmd.Start` gagal dgn pesan generik; `get_env` memory_mb uint64 bisa overflow int di 32-bit).

## Implementasi Terpilih (Ronde 18)
Perbaikan `resolveWorkdir` di `internal/ai/fs.go`: hasil kini dipastikan ada + berupa direktori via `os.Stat` (cwd tak ada → "cwd tidak ditemukan", file → "cwd bukan direktori", keduanya + hint `list_dir`; workspace kosong juga dicek) agar `exec run` tak gagal di `cmd.Start` dengan error chdir generik. Teruji: `gofmt` bersih, `go vet ./internal/ai/` bersih, `go test ./internal/ai/ -count=1` OK + test baru `TestResolveWorkdirValidatesDir` di `workdir_test.go`. Dok: AGENTS.md + README.md sinkron.

## Slot Ronde 19 (diganti tugas KHUSUS — dikerjakan, slot bug dibatalkan)
Tugas khusus: tambah 2 tools web ala picoclaw (web_search + web_fetch), bukan slot bug Ronde 19 semula.

## Implementasi Terpilih (Ronde 19)
Tambah `web_search` (query wajib non-kosong, count default 5 clamp 1-10; Yahoo HTML `search.yahoo.com/search?p=` UA browser timeout 15 dtk, parser regex generik judul+URL+snippet, fallback Bing HTML `bing.com/search?q=`; gagal keduanya → error jelas) + `web_fetch` (url wajib http/https publik — tolak file:// dan host lokal/private; max_chars default 8000 clamp 1000-20000; timeout 20 dtk, body cap ~100KB, strip HTML via regex + UnescapeString + normalisasi whitespace + marker truncate) di `internal/ai/web.go` (stdlib saja, tanpa dependensi baru), didaftarkan di `BuildTools` (`internal/ai/tools.go`, 9 → 11 tools, hook `opts.OnTool` ikut pola `mk`). System prompt `internal/prompt/prompt.go` (11 tools + aturan kapan pakai web_search/web_fetch). Teruji: `gofmt` bersih, `go vet ./internal/ai/ ./internal/prompt/` bersih, test baru `internal/ai/web_test.go` (query kosong ditolak, URL non-http/lokal ditolak, clamp count/max_chars, strip HTML, parse+format pure, hook OnTool, registrasi) + update `TestToolCount` 9 → 11 — semua tanpa akses network. Dok: AGENTS.md + README.md sinkron. (Selesai)

## Slot Ronde 20
Cari bug/inkonsistensi berikutnya (kandidat sisa: `get_env` memory_mb uint64 bisa overflow int di 32-bit — `m.Alloc / 1024 / 1024` untyped constant division; cek juga `clampMemMB` tanpa batas atas).
