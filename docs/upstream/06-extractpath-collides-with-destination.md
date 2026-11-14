### Environment
Device and OS: any
App version: `main` at [`1f516c0`](https://github.com/zarf-dev/zarf/commit/1f516c0fce0c), and every release carrying the current `assemble` package
Kubernetes distro being used: n/a - this is `zarf package create`, before any cluster
Other: n/a

### Steps to reproduce
1. Take any archive whose member path begins with a directory of the same name as the file you want out of it. Zarf's own component tarballs have this shape: `components/zarf-injector.tar` inside `ghcr.io/zarf-dev/packages/init:v0.89.0` holds exactly one member, `zarf-injector/files/0/zarf-injector`.

2. Declare it as a component file, with a destination named after the binary:

   ```yaml
   components:
     - name: tools
       files:
         - source: ./zarf-injector.tar
           extractPath: zarf-injector/files/0/zarf-injector
           target: /usr/local/bin/zarf-injector
           executable: true
   ```

3. `zarf package create`

### Expected result
`files/0/zarf-injector` in the built package is the extracted binary.

### Actual Result
The build fails, naming a path that appears nowhere in the component:

```text
rename .../files/0/zarf-injector/files/0/zarf-injector .../files/0/zarf-injector: file exists
```

The extraction writes into the destination's **parent** directory, so a member path beginning `zarf-injector/` creates a *directory* at exactly the path the member is then renamed onto. [The rename](https://github.com/zarf-dev/zarf/blob/1f516c0fce0c/src/pkg/packager/assemble/assemble.go#L525-L532) cannot replace a non-empty directory with a file:

```go
if file.ExtractPath != "" {
    // Make sure dst reflects the actual file or directory.
    updatedExtractedFileOrDir := filepath.Join(destinationDir, file.ExtractPath)
    if updatedExtractedFileOrDir != dst {
        if err := os.Rename(updatedExtractedFileOrDir, dst); err != nil {
            return fmt.Errorf(lang.ErrWritingFile, dst, err)
        }
    }
}
```

The same block appears in [`assembleSkeletonComponent`](https://github.com/zarf-dev/zarf/blob/1f516c0fce0c/src/pkg/packager/assemble/assemble.go#L797-L804), so both build paths are affected.

Nothing about this is specific to where the archive came from - the local, URL and extraction branches all converge on this tail - and it is not specific to `.tar` either. Any member path whose first segment equals `path.Base(target)` reaches it.

### Visual Proof (screenshots, videos, text, etc)
The error above, observed on code structurally identical to the block quoted. The message is the whole difficulty: it names `files/0/zarf-injector` twice at different depths, mentions a directory the component never wrote, and gives no indication that `extractPath` and `target` are what interact.

### Severity/Priority
Medium. It is a hard failure rather than a silent one, so nothing ships wrong - but the error points away from the cause, and the shape that triggers it is the shape of zarf's own component tarballs, so anyone repackaging one meets it.

### Additional Context
Two shapes of fix, and the second is probably better:

- Move the extracted member aside before clearing the tree it came in, then rename it into place.
- Extract into a directory of its own and **copy** the member to the destination. The extraction then never writes at the destination path at all, so the collision cannot occur, and a second entry wanting a different member of the same archive is served from the same extraction - which is the fix for the repeated-extraction defect filed alongside this one.
