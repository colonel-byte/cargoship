# Index

[readme](index.md)
[security](security.md)

-----------

# Guides

- [age-encryption](guides/age-encryption.md)
- [ansible-container](guides/ansible-container.md)
- [ansible-inv](guides/ansible-inv.md)
- [ansible-module](guides/ansible-module.md)
- [firewall](guides/firewall.md)
- [multi-architecture](guides/multi-architecture.md)
- [package-values](guides/package-values.md)
- [profile-concurrency](guides/profile-concurrency.md)
- [registry-override](guides/registry-override.md)
- [removing-hosts](guides/removing-hosts.md)
- [setup-inv](guides/setup-inv.md)
- [signing-packages](guides/signing-packages.md)
- [vault-encryption](guides/vault-encryption.md)


-----------

# Commands

- [cargoship](commands/cargoship.md)
  - [apply](commands/cargoship_apply.md)
  - [create](commands/cargoship_create.md)
  - [engine-config-sync](commands/cargoship_engine-config-sync.md)
  - [inventory](commands/cargoship_inventory.md)
    - [from-ansible](commands/cargoship_inventory_from-ansible.md)
  - [kube-config](commands/cargoship_kube-config.md)
  - [prepare](commands/cargoship_prepare.md)
  - [publish](commands/cargoship_publish.md)
  - [pull](commands/cargoship_pull.md)
  - [reset](commands/cargoship_reset.md)
  - [schema](commands/cargoship_schema.md)
  - [sha256sum](commands/cargoship_sha256sum.md)
  - [sign](commands/cargoship_sign.md)
  - [validate](commands/cargoship_validate.md)
  - [vault](commands/cargoship_vault.md)
    - [decrypt-file](commands/cargoship_vault_decrypt-file.md)
    - [decrypt-path](commands/cargoship_vault_decrypt-path.md)
    - [decrypt](commands/cargoship_vault_decrypt.md)
    - [encrypt-file](commands/cargoship_vault_encrypt-file.md)
    - [encrypt-path](commands/cargoship_vault_encrypt-path.md)
    - [encrypt](commands/cargoship_vault_encrypt.md)
    - [keygen](commands/cargoship_vault_keygen.md)
    - [rekey](commands/cargoship_vault_rekey.md)
  - [version](commands/cargoship_version.md)


-----------

# Schema

- [cluster](schema/cluster.md)
- [config](schema/config.md)
- [distro](schema/distro.md)


-----------

# Ansible

- [collection](ansible/collection.md)
- [modules](ansible/modules.md)
  - [apply](ansible/module_apply.md)
  - [engine_config_sync](ansible/module_engine_config_sync.md)
  - [kube_config](ansible/module_kube_config.md)
  - [prepare](ansible/module_prepare.md)
  - [reset](ansible/module_reset.md)
- [roles](ansible/roles.md)
  - [cluster](ansible/role_cluster.md)


-----------

# Phases

- [apply](phases/apply.md)
- [engine-config-sync](phases/engine-config-sync.md)
- [kube-config](phases/kube-config.md)
- [prepare](phases/prepare.md)
- [reset](phases/reset.md)


-----------

# Golang

- [api](golang/api.md)
  - [v1alpha1](golang/api/zarf.dev/v1alpha1.md)
    - [cluster](golang/api/zarf.dev/v1alpha1/cluster.md)
    - [distro](golang/api/zarf.dev/v1alpha1/distro.md)
- [action](golang/pkg/action.md)
- [coci](golang/pkg/coci.md)
  - [layers](golang/pkg/coci/layers.md)
- [distro](golang/pkg/distro.md)
- [extract](golang/pkg/engineconfig/extract.md)
- [gen](golang/pkg/engineconfig/gen.md)
  - [v1_31](golang/pkg/engineconfig/gen/k3s/v1_31.md)
  - [v1_32](golang/pkg/engineconfig/gen/k3s/v1_32.md)
  - [v1_33](golang/pkg/engineconfig/gen/k3s/v1_33.md)
  - [v1_34](golang/pkg/engineconfig/gen/k3s/v1_34.md)
  - [v1_35](golang/pkg/engineconfig/gen/k3s/v1_35.md)
  - [v1_36](golang/pkg/engineconfig/gen/k3s/v1_36.md)
  - [v1_37](golang/pkg/engineconfig/gen/k3s/v1_37.md)
  - [v1_31](golang/pkg/engineconfig/gen/rke2/v1_31.md)
  - [v1_32](golang/pkg/engineconfig/gen/rke2/v1_32.md)
  - [v1_33](golang/pkg/engineconfig/gen/rke2/v1_33.md)
  - [v1_34](golang/pkg/engineconfig/gen/rke2/v1_34.md)
  - [v1_35](golang/pkg/engineconfig/gen/rke2/v1_35.md)
  - [v1_36](golang/pkg/engineconfig/gen/rke2/v1_36.md)
  - [v1_37](golang/pkg/engineconfig/gen/rke2/v1_37.md)
- [firewall](golang/pkg/firewall.md)
- [helmvalues](golang/pkg/helmvalues.md)
- [helpers](golang/pkg/helpers.md)
- [images](golang/pkg/images.md)
- [lint](golang/pkg/lint.md)
- [node](golang/pkg/node.md)
- [archive](golang/pkg/oci/archive.md)
- [platform](golang/pkg/oci/platform.md)
- [assemble](golang/pkg/packager/assemble.md)
- [layout](golang/pkg/packager/layout.md)
- [load](golang/pkg/packager/load.md)
- [phase](golang/pkg/phase.md)
- [retry](golang/pkg/retry.md)
- [schema](golang/pkg/schema.md)
- [utils](golang/pkg/utils.md)
  - [build](golang/pkg/utils/build.md)
- [types](golang/types.md)
  - [distrocfg](golang/types/distrocfg.md)
    - [registry](golang/types/distrocfg/registry.md)
  - [os](golang/types/os.md)
    - [linux](golang/types/os/linux.md)
      - [enterpriselinux](golang/types/os/linux/enterpriselinux.md)


-----------

# Development

<!-- Excluded from the print page (print.html) by docs/css/print.css. -->

- [build-flags](dev/build-flags.md)
- [e2e-phase-tests](dev/e2e-phase-tests.md)
- [e2e-tests](dev/e2e-tests.md)
- [fuzz-tests](dev/fuzz-tests.md)
- [goreleaser](dev/goreleaser.md)
- [mage-test-manual](dev/mage-test-manual.md)
- [mage](dev/mage.md)
- [shell-completion](dev/shell-completion.md)
- [thirdparty-src](dev/thirdparty-src.md)


-----------

# Workflows

<!-- Excluded from the print page (print.html) by docs/css/print.css. -->

- [check-ansible-requirements](workflows/check-ansible-requirements.md)
- [check-base-image-label](workflows/check-base-image-label.md)
- [check-go-mod](workflows/check-go-mod.md)
- [check-pins](workflows/check-pins.md)
- [codeql](workflows/codeql.md)
- [commitlint](workflows/commitlint.md)
- [dependabot-validate](workflows/dependabot-validate.md)
- [deploy-book](workflows/deploy-book.md)
- [e2e-cluster](workflows/e2e-cluster.md)
- [e2e](workflows/e2e.md)
- [fix-vendor-osv](workflows/fix-vendor-osv.md)
- [pre-commit](workflows/pre-commit.md)
- [prune-example-pr-packages](workflows/prune-example-pr-packages.md)
- [publish-example](workflows/publish-example.md)
- [refresh-examples](workflows/refresh-examples.md)
- [release-please](workflows/release-please.md)
- [release](workflows/release.md)
- [scan-lint](workflows/scan-lint.md)
- [scorecard](workflows/scorecard.md)
- [test-build-containers](workflows/test-build-containers.md)


-----------

# Agent

<!-- Excluded from the print page (print.html) by docs/css/print.css. -->

- [ai-usage](agent/ai-usage.md)
- [choice-age-encryption](agent/choice-age-encryption.md)
- [choice-ansible-collection-pins](agent/choice-ansible-collection-pins.md)
- [choice-ansible-module](agent/choice-ansible-module.md)
- [choice-binary-artifact-opa-wasm](agent/choice-binary-artifact-opa-wasm.md)
- [choice-e2e-stage-split](agent/choice-e2e-stage-split.md)
- [choice-image-index](agent/choice-image-index.md)
- [choice-in-memory-oci-registry](agent/choice-in-memory-oci-registry.md)
- [choice-managed-manifest-glob](agent/choice-managed-manifest-glob.md)
- [choice-nftables-backend](agent/choice-nftables-backend.md)
- [choice-osv-vendor-overrides](agent/choice-osv-vendor-overrides.md)
- [choice-phase-e2e-tests](agent/choice-phase-e2e-tests.md)
- [choice-preferred-firewall](agent/choice-preferred-firewall.md)
- [choice-removed-hosts](agent/choice-removed-hosts.md)
- [choice-unpinnable-hosts](agent/choice-unpinnable-hosts.md)
- [choice-vault-library](agent/choice-vault-library.md)
- [design-config-codegen](agent/design-config-codegen.md)
- [ideas-inventory-sources](agent/ideas-inventory-sources.md)
- [todo-pkg-action-unit-tests](agent/todo-pkg-action-unit-tests.md)
- [todo-pkg-coci-registry-tests](agent/todo-pkg-coci-registry-tests.md)


-----------

# Misc

<!-- Excluded from the print page (print.html) by docs/css/print.css. -->

- [changelog](misc/changelog.md)
