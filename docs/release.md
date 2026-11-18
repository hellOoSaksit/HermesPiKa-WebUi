# Releasing hpk

A release is a tag. CI does the building; a person checks and publishes.

1. **Tag.** On a clean `main` with green CI: `git tag v0.1.0 && git push origin v0.1.0`
   (use the version in `CHANGELOG.md`). The `release` workflow runs the tests,
   then GoReleaser builds every platform and creates a **draft** GitHub release.
   The `docker` job then pushes the image to GHCR (`:<version>`, and `:latest`
   only for a tag without a `-` such as `-rc1`).
2. **Check the draft assets.** `hpk.exe`, six archives (`hpk_<version>_{linux,darwin}_{amd64,arm64}.tar.gz`,
   `hpk_<version>_windows_{amd64,arm64}.zip`), `install.sh`, `install.ps1`,
   `compose.release.yaml`, `checksums.txt` and the SBOMs. Download one archive and run
   `sha256sum --ignore-missing -c checksums.txt`.
3. **Make the GHCR package public (once).** A package pushed for the first time
   is private, so `docker compose` could not pull it. On GitHub open your profile
   or organisation, **Packages**, `hpk-hermes`, **Package settings**, then
   **Change visibility**, and choose **Public**. It stays public for later versions.
4. **Pin the next version.** `docker/compose.release.yaml` names the image with
   the version it was released with. After a release, bump that tag for the next
   version (the release's own copy was already built from the tag).
5. **Publish the draft** on the Releases page. Then `releases/latest/download/...`
   (used by the install commands) points at it.

CI also builds a snapshot of the whole release on every pull request
(`release-config` job) and checks that every file in step 2 is there.
