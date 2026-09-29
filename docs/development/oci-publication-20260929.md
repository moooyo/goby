# OCI delivery publication - September 29, 2026

Status: **software and selected AMD OCI deliveries are integrated and published**.

| Delivery | Commit merged and pushed to `origin/main` | Publication evidence |
| --- | --- | --- |
| Linux amd64 software archive and Compose | `1e34ae33509d9719561db9bf20df8acdddf09014` | Completed software delivery and recorded push/readback |
| Linux amd64 selected AMD archive and Compose | `6c574ca5708051f0e48e18ebd99903688a3e13ee` | Fast-forward from `1e34ae3`, successful `git push origin main`, and exact `git ls-remote origin refs/heads/main` readback |

The AMD delivery includes recipe commits `14cdb28`, `29f00ef` and `82741b2`.
It retains the software delivery's application binary from source `b9bfa7c`;
the new image and final deployment/acceptance companions have separate identities.
Use the [software acceptance record](oci-delivery-20260929.md) and
[AMD acceptance record](oci-amd-delivery-20260929.md), with their linked result
manifests, for exact artifact and runtime evidence.

Those acceptance records were frozen before Git integration and explicitly
handled publication separately. Their build-stage or publication-pending wording
is not a current outstanding task. This later publication record completes that
step without editing raw receipts, changing image bytes, or relabeling failures.

Both archives and their Compose guides are delivered locally. No container
registry publication or production deployment occurred. Verification resources
are closed as recorded; retained data, original failures and unrelated primary
checkout changes were preserved.
