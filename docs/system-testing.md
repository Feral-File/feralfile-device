# FF‑X1 System Testing & QA

*Rev 0.1 – 2025-05-28*

This document lays out **what we test, when we add tests, and how large‑language models help**—using a pure Kanban flow.

---

## 1  Guiding Principle

**Write a unit test as soon as breaking that function would delay the team more than an hour.**

This keeps us pragmatic: prototype work moves fast, but once an API stabilises we freeze behaviour with a test.

---

## 2  Where AI/LLMs Fit

| Stage | LLM prompt example | Team responsibility |
| :---- | :---- | :---- |
| **Unit‑test scaffolding** | “Generate table‑driven Go tests for `ParsePairingJSON` covering empty SSID, long pwd, invalid UTF‑8.” | Read code, adjust assertions, name sub‑tests clearly |
| **Mocking external calls** | “Write a Go test using `exec.CommandContext` stub to fake `nmcli` success & failure.” | Ensure flag list matches real CLI; maintain helper |
| **Fuzzing harness** | “Create a `go test fuzz` harness for the BLE payload decoder; seed corpus with 6 valid examples.” | Set CI timeout &  coverage target |
| **Systemd unit tests** | “Write a `bats-core` script asserting `feral-state.service` returns correct isolate target.” | Provide QEMU image to run against |
| **CI boilerplate** | “Draft GitHub Actions YAML that runs `go test ./...` and posts a coverage badge.” | Validate paths, artefact retention |

LLMs produce **scaffolds**, not final tests—developers review every diff like any PR.

---

## 3  Phase Tags Used in Kanban

* **phase‑stabilisation** – Immediately after prototype freeze  
* **phase‑public‑beta** – Before OTA goes to external testers  
* **phase‑production** – Factory line, large batch QA

Cards carry these tags so we can filter the board without dating language.

---

## 4  Definition of Done

| Phase | Reliability | Performance | Security / Privacy | Environment |
| :---- | :---- | :---- | :---- | :---- |
| **Stabilisation** | 24 h smoke passes (`fps ≥ 55`, CPU \< 85 °C) | Avg frame latency \< 16 ms | BLE pairing creds stored 0600; OTA sig verified | 23 °C lab, Wi-Fi loss \< 0.1 % |
| **Public Beta** | 72 h unattended run recovers from forced OTA abort & power-loss reboot | FPS ≥ 58 @ 4 K; peak temp \< 80 °C | PSK rotation OK; no plaintext creds in journal; CVE scan clean | 30 °C ambient, 5 % packet loss, captive-portal join |
| **Production** | 168 h burn-in loop, zero GPU stalls, watchdog ≤ 1 reboot | FPS ≥ 60 sustained | Secure-boot checksum match; OTA downgrade blocked; PSU brown-out survives | 40 °C chamber, fan vents 50 % blocked |

---

### 5  CI stages summary

| Stage | Trigger | Tests |
| :---- | :---- | :---- |
| **quick** | every PR | `go vet`, unit tests |
| **nightly** | 02:00 UTC | fuzz harness, markdown‑lint |
| **full** | tag `beta` | QEMU install, OTA forward/rollback |
| **factory** | make prod‑img | self‑test assertion script |

---

### 6  Ownership & Review

* **Any code with `// LLM‑GENERATED` header requires two human reviewers.**  
* Test failures block merge to `main`.  
* Docs for new tests live under `/docs/testing/`.
