# Değişiklikler

Sürümler [Semantic Versioning](https://semver.org/lang/tr/) izler. `0.x` boyunca minor
sürümler kırıcı değişiklik içerebilir.

---

## 0.9.0 — 2026-08-25

İlk paketlenmiş sürüm. Dokuz faz tamamlandı; Faz 10 (sertleştirme ve yayın) bu sürümle
kapandı. Üretimde kullanılmadan önce `docs/SECURITY.md`'deki **kalan riskler** okunmalıdır.

### Kurulum

- **Tek imaj:** distroless, non-root (uid 65532), kabuksuz, salt-okunur kök. `kubectl`
  v1.36.4 ve `helm` v4.2.3 içinde — cluster terminali onları çalıştırır
- **Kubby şemasını kendisi kurar.** Ayrı bir migration adımı yok; yükseltme bir imaj
  etiketi değiştirmekten ibaret
- **Docker Compose** — `deploy/compose/`, iki container
- **Helm chart** — `deploy/helm/kubby/`; securityContext, NetworkPolicy, PDB, ve joker
  içermeyen en az yetkili ClusterRole
- `/readyz` şemayı da doğrular: ulaşılabilir bir veritabanı kullanılabilir demek değildir

### Cluster yönetimi

- Çok-cluster gezinme, her tür için sunucu tarafı projeksiyon
- Sağlık paneli: bozuk olan her şey tek ekranda, sebebiyle
- Yazma yolu: apply (dry-run + diff), scale, restart, rollback, delete, evict,
  cordon/drain, CronJob trigger/suspend — hepsi ArgoCD sahiplik farkındalığıyla
- Canlı güncelleme (SSE), iki yönlü sahiplik grafiği
- Pod kabuğu, node shell, ephemeral debug container, port-forward
- **Terminal** — `kubectl` ve `helm`'e kilitli, dosya sürüklenebilir
- Ctrl+K ile filo geneli arama; ulaşılamayan cluster **adıyla** raporlanır
- Helm release görünümü (değerler ve revizyon geçmişi)
- Prometheus entegrasyonu: cluster başına bağlantı, dashboard

### Güvenlik ve işletim

- argon2id · TOTP · kademeli kilitleme · CSRF · kapatılamaz audit
- Zarf şifreleme, satıra bağlı ciphertext; `make rotate-key`
- Audit gönderimi: Elasticsearch (data stream desteğiyle), Loki, HTTP/NDJSON
- `/metrics` — Kubby'nin kendi metrikleri, hiçbir zaman kimlik doğrulamasız
- `make config-export` / `config-restore` — parola korumalı şifreli arşiv
- CI: trivy, semgrep, gitleaks, npm audit, govulncheck — hepsi build'i düşürür

### Bu sürümde düzeltilen güvenlik kusurları

Faz 10'un güvenlik geçişinde bulundu:

- **Cluster başına grant seviyesi yazmayı engellemiyordu.** `read` grant'i yalnızca
  listelemede kullanılıyor, yazma yolunda hiç sorulmuyordu — prod bir cluster'da birine
  "read" vermek hiçbir şeyi durdurmuyordu
- **Port-forward proxy'si hata yolunda CSP'siz yanıt veriyordu**
- **Bozuk nesne adı 502 dönüyordu** — okuyucuyu çalışan bir cluster'ı incelemeye
  yönlendiriyordu; artık 400 ve sebebi
- **Ayar kaydetmek `/healthz`'i veritabanına bağımlı yapmıştı**; arka plan görevi panic'te
  tüm süreci düşürebiliyordu

### Bilinen sınırlar

- Tek replika hedeflenir (ADR-016)
- OIDC yok; kimlik yerel hesaplardır
- Loglar yalnızca Pod içindir
- Terminal cluster'ın kimlik bilgisini taşır — `docs/SECURITY.md` §1
- `monaco-editor` bağımlılığında orta seviye bir XSS danışmanlığı — `docs/SECURITY.md` §4

---

## Yayın nasıl yapılır

```bash
make test lint                                    # her şey yeşil olmalı
make release VERSION=0.9.0 IMAGE_REGISTRY=<hedef>  # derle, doğrula, push
make tag VERSION=0.9.0                            # temiz ağaçta etiketle
git push origin v0.9.0
```

`make release` imajı push etmeden önce doğrular: doğru `kubectl`/`helm` sürümleri, uid
65532, kabuk yok. `make tag` kirli bir ağaçta çalışmaz — orada atılan bir etiket,
derlenen şeyi içermeyen bir commit'i gösterir.

`latest` etiketi **üretilmez**: hangi sürümün çalıştığını söyleyemez ve geri dönmeyi
imkânsız kılar.
