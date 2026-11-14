### Environment
Device and OS: any
App version: `main` at [`1f516c0`](https://github.com/zarf-dev/zarf/commit/1f516c0fce0c), and every release carrying the current `assemble` package
Kubernetes distro being used: n/a - this is `zarf package create`, before any cluster
Other: n/a

### Steps to reproduce
1. Write a component that takes several files out of one release archive, which is the normal shape for packaging an engine or a toolchain:

   ```yaml
   components:
     - name: tools
       files:
         - source: https://github.com/etcd-io/etcd/releases/download/v3.6.6/etcd-v3.6.6-linux-amd64.tar.gz
           extractPath: etcd-v3.6.6-linux-amd64/etcd
           target: /usr/local/bin/etcd
         - source: https://github.com/etcd-io/etcd/releases/download/v3.6.6/etcd-v3.6.6-linux-amd64.tar.gz
           extractPath: etcd-v3.6.6-linux-amd64/etcdctl
           target: /usr/local/bin/etcdctl
         - source: https://github.com/etcd-io/etcd/releases/download/v3.6.6/etcd-v3.6.6-linux-amd64.tar.gz
           extractPath: etcd-v3.6.6-linux-amd64/etcdutl
           target: /usr/local/bin/etcdutl
   ```

2. Run `zarf package create` and watch the network, or time it.

### Expected result
The archive is fetched once and read once, and three files come out of it.

### Actual Result
It is fetched **three times** and decompressed **three times** - once per file entry.

[`assemblePackageComponent`](https://github.com/zarf-dev/zarf/blob/1f516c0fce0c/src/pkg/packager/assemble/assemble.go#L421) loops over `component.Files`, and both the temporary directory and the download sit [inside that loop](https://github.com/zarf-dev/zarf/blob/1f516c0fce0c/src/pkg/packager/assemble/assemble.go#L468-L498):

```go
for filesIdx, file := range component.Files {
    ...
    tmpDir, err := utils.MakeTempDir(config.CommonOptions.TempDirectory)
    ...
    compressedFile := filepath.Join(tmpDir, compressedFileName)
    if err := utils.DownloadToFile(ctx, file.Source, compressedFile); err != nil {
```

[`utils.DownloadToFile`](https://github.com/zarf-dev/zarf/blob/1f516c0fce0c/src/pkg/utils/network.go#L56) has no cache, so nothing deduplicates the fetch. The decompression repeats for the same reason, and repeats in [`assembleSkeletonComponent`](https://github.com/zarf-dev/zarf/blob/1f516c0fce0c/src/pkg/packager/assemble/assemble.go#L776-L795) too, which reads local paths and so pays only the extraction cost.

The cost scales with the number of files, not with the number of archives: a component taking ten files out of one 40 MiB tarball transfers 400 MiB and decompresses it ten times.

### Visual Proof (screenshots, videos, text, etc)
Measured on code structurally identical to the loop above - ten files out of two archives, every download already local, so the figure isolates the repeated decompression alone:

|                            | wall   | CPU   |
| -------------------------- | ------ | ----- |
| one extraction per file    | 11.70s | 7.43s |
| one extraction per archive | 4.11s  | 3.19s |

With the downloads included rather than pre-cached, the first row grows by nine further transfers of the same bytes.

### Severity/Priority
Low severity, and it costs every `create` of a component shaped this way. It is bandwidth and wall-clock rather than correctness, but it is paid on the kind of build that is often metered or slow.

### Additional Context
The extraction already supports the fix. [`DecompressOpts.Files`](https://github.com/zarf-dev/zarf/blob/1f516c0fce0c/src/pkg/archive/archive.go#L175) is a set, and [`filterHandler`](https://github.com/zarf-dev/zarf/blob/1f516c0fce0c/src/pkg/archive/archive.go#L364) pulls every match out of a single walk - so one pass can serve every entry that shares an archive. What does not use it is the per-entry call structure.

The shape of a fix: collect what each archive is asked for before staging anything, keyed on the resolved source, then fetch and decompress once per archive and copy each member to its destination. Copying rather than renaming also resolves a separate defect filed alongside this one, where an `extractPath` whose leading segment matches the destination's base name collides with the directory the extraction just created.
