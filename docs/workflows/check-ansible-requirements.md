# check-ansible-requirements.yaml

**Triggers:** pull requests, merge groups.

Checks that every pinned Ansible collection version supports the `ansible-core` range declared in the collection's `meta/runtime.yml`. It does this by asking galaxy.ansible.com what each pinned version's `requires_ansible` field says, via `mage test:ansibleRequirements`.

This is the one check workflow that reaches the network, since no dependency bot can validate this constraint on its own. It does not check whether pins are the newest available — an upstream release from this morning should not fail an unrelated PR. Run `mage generate:ansibleRequirements` locally to move pins forward.

See `docs/agent/choice-ansible-collection-pins.md` for the reasoning.
