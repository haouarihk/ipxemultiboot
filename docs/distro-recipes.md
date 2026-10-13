# Distro recipes

When iPXE Multiboot scans an ISO, it runs a list of *detectors* (`internal/distro/distro.go`). The
first one that recognizes the ISO's layout returns a **recipe**:

```go
type Recipe struct {
	Family  string   // shown in the UI, e.g. "archiso"
	Kernel  string   // path inside the ISO
	Initrds []string // paths inside the ISO, loaded in order
	Args    string   // kernel command line
}
```

The boot menu then loads the kernel and initrds **directly out of the ISO** over HTTP
(`/iso/<id>/<path>`), with `Args` as the command line. These placeholders are expanded:

| Placeholder | Expands to |
| --- | --- |
| `{iso}` | URL of the raw ISO file, e.g. `http://10.0.0.1:8080/isos/ubuntu-24.04` |
| `{base}` | URL prefix for files inside the ISO, e.g. `http://10.0.0.1:8080/iso/ubuntu-24.04` |

iPXE settings such as `${netX/mac:hexhyp}` can be used too; iPXE expands them on the client.

The trick is always the same: tell the distro's initramfs to fetch its root filesystem over HTTP
instead of from a CD. Most initramfs implementations already support this for netbooting:

| Initramfs | Parameter |
| --- | --- |
| archiso | `archiso_http_srv={base}/` |
| casper (Ubuntu) | `url={iso}` |
| live-boot (Debian) | `fetch={base}/live/filesystem.squashfs` |
| dracut (Fedora/RHEL) | `root=live:{base}/LiveOS/squashfs.img` |
| Anaconda | `inst.repo={base}/` |
| Alpine | `modloop={base}/boot/modloop-lts alpine_repo={base}/apks` |

## Adding a recipe

1. Find the kernel and initrd paths and the original kernel command line in the ISO:

   ```sh
   go run ./cmd/isoinspect my.iso ls /
   go run ./cmd/isoinspect my.iso cat /boot/grub/grub.cfg
   ```

2. Look up the distro's netboot documentation for its "load root over HTTP" parameter.
3. Add a detector function in `internal/distro/distro.go` and put it in `detectors`, before any
   more generic detector that would also match.
4. Add a test case to `internal/distro/distro_test.go` with the files that identify the layout.
5. Boot it for real: drop the ISO into `testdata/isos/` and run
   `scripts/qemu-test.sh bios full` (or `uefi`), then drive the VM with `scripts/qmp.py`.
6. Update the table in the README and open a pull request. Mention which ISO versions you booted.
