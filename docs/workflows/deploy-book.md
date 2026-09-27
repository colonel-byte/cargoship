# deploy-book.yaml

**Triggers:** push to `main`.

Builds the project's mdBook documentation site and deploys it to GitHub Pages by pushing the built output to the `gh-pages` branch. This is the only workflow that writes with `contents: write` permission, and it never touches pull requests - it only runs after a merge to `main`.
