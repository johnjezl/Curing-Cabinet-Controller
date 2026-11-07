# Evidence

The raw evidence behind the critic rounds (ADRs, docs/eval/ and docs/eval/critic/ cite paths such as
`evidence/a/...`) is not kept in this repository. It is preserved unchanged in Dropbox:

- **Link:** https://www.dropbox.com/scl/fo/5qre1erxyepea7tp7jrck/ALyhZIAv2wsYUZ-F7hh5hCU?rlkey=25qlnq0i5jbuk6ojk4dydbkds&st=bz4x019e&dl=0
- **Path:** Projects/Curing Cabinet/Controller/evidence/

| Folder | Part | Files | Size | Regenerate with |
|---|---|---|---|---|
| `evidence/a` | control core and simulated plant | 1166 | 276 MB | `deploy/evidence-a.sh` |
| `evidence/b` | drivers and provisioning (rig) | 38 | 316 KB | `deploy/evidence-b.sh`, `deploy/evidence-b-power-cut.sh` |
| `evidence/c` | front panel | 356 | 22 MB | `deploy/evidence-c.sh` |
| `evidence/d` | service, boot, watchdog, power loss (rig) | 326 | 27 MB | `deploy/evidence-d.sh`, `deploy/evidence-d-power-cut.sh`, `deploy/cure-compressed.sh` |

**Checksums:** [`evidence.sha256`](evidence.sha256) lists the SHA-256 of all 1886 files as judged by
the critics. After downloading, check them from the folder that contains `evidence/`:

    sha256sum -c docs/evidence.sha256

Regenerating gives new evidence from the current code, not the copies the critics judged. The rig
scripts also need the test rig and labctl.
