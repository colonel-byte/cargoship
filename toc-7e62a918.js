// Populate the sidebar
//
// This is a script, and not included directly in the page, to control the total size of the book.
// The TOC contains an entry for each page, so if each page includes a copy of the TOC,
// the total size of the page becomes O(n**2).
class MDBookSidebarScrollbox extends HTMLElement {
    constructor() {
        super();
    }
    connectedCallback() {
        this.innerHTML = '<ol class="chapter"><li class="chapter-item expanded "><span class="chapter-link-wrapper"><a href="index.html">readme</a></span></li><li class="chapter-item expanded "><li class="spacer"></li></li><li class="chapter-item expanded "><li class="part-title">Guides</li></li><li class="chapter-item expanded "><span class="chapter-link-wrapper"><a href="guides/profile-concurrency.html">profile-concurrency</a></span></li><li class="chapter-item expanded "><span class="chapter-link-wrapper"><a href="guides/registry-override.html">registry-override</a></span></li><li class="chapter-item expanded "><span class="chapter-link-wrapper"><a href="guides/setup-inv.html">setup-inv</a></span></li><li class="chapter-item expanded "><li class="spacer"></li></li><li class="chapter-item expanded "><li class="part-title">Commands</li></li><li class="chapter-item expanded "><span class="chapter-link-wrapper"><a href="commands/cargoship.html">cargoship</a></span><ol class="section"><li class="chapter-item expanded "><span class="chapter-link-wrapper"><a href="commands/cargoship_apply.html">apply</a></span></li><li class="chapter-item expanded "><span class="chapter-link-wrapper"><a href="commands/cargoship_create.html">create</a></span></li><li class="chapter-item expanded "><span class="chapter-link-wrapper"><a href="commands/cargoship_engine-config-sync.html">engine-config-sync</a></span></li><li class="chapter-item expanded "><span class="chapter-link-wrapper"><a href="commands/cargoship_kube-config.html">kube-config</a></span></li><li class="chapter-item expanded "><span class="chapter-link-wrapper"><a href="commands/cargoship_prepare.html">prepare</a></span></li><li class="chapter-item expanded "><span class="chapter-link-wrapper"><a href="commands/cargoship_publish.html">publish</a></span></li><li class="chapter-item expanded "><span class="chapter-link-wrapper"><a href="commands/cargoship_pull.html">pull</a></span></li><li class="chapter-item expanded "><span class="chapter-link-wrapper"><a href="commands/cargoship_reset.html">reset</a></span></li><li class="chapter-item expanded "><span class="chapter-link-wrapper"><a href="commands/cargoship_sha256sum.html">sha256sum</a></span></li><li class="chapter-item expanded "><span class="chapter-link-wrapper"><a href="commands/cargoship_sign.html">sign</a></span></li><li class="chapter-item expanded "><span class="chapter-link-wrapper"><a href="commands/cargoship_vault-encrypt.html">vault-encrypt</a></span></li><li class="chapter-item expanded "><span class="chapter-link-wrapper"><a href="commands/cargoship_version.html">version</a></span></li></ol><li class="chapter-item expanded "><li class="spacer"></li></li><li class="chapter-item expanded "><li class="part-title">Phases</li></li><li class="chapter-item expanded "><span class="chapter-link-wrapper"><a href="phases/apply.html">apply</a></span></li><li class="chapter-item expanded "><span class="chapter-link-wrapper"><a href="phases/engine-config-sync.html">engine-config-sync</a></span></li><li class="chapter-item expanded "><span class="chapter-link-wrapper"><a href="phases/kube-config.html">kube-config</a></span></li><li class="chapter-item expanded "><span class="chapter-link-wrapper"><a href="phases/prepare.html">prepare</a></span></li><li class="chapter-item expanded "><span class="chapter-link-wrapper"><a href="phases/reset.html">reset</a></span></li><li class="chapter-item expanded "><li class="spacer"></li></li><li class="chapter-item expanded "><li class="part-title">Development</li></li><li class="chapter-item expanded "><span class="chapter-link-wrapper"><a href="dev/dagger.html">dagger</a></span></li><li class="chapter-item expanded "><span class="chapter-link-wrapper"><a href="dev/mage.html">mage</a></span></li><li class="chapter-item expanded "><span class="chapter-link-wrapper"><a href="dev/vault-library-choice.html">vault-library-choice</a></span></li><li class="chapter-item expanded "><li class="spacer"></li></li><li class="chapter-item expanded "><li class="part-title">Misc</li></li><li class="chapter-item expanded "><span class="chapter-link-wrapper"><a href="misc/changelog.html">changelog</a></span></li></ol>';
        // Set the current, active page, and reveal it if it's hidden
        let current_page = document.location.href.toString().split('#')[0].split('?')[0];
        if (current_page.endsWith('/')) {
            current_page += 'index.html';
        }
        const links = Array.prototype.slice.call(this.querySelectorAll('a'));
        const l = links.length;
        for (let i = 0; i < l; ++i) {
            const link = links[i];
            const href = link.getAttribute('href');
            if (href && !href.startsWith('#') && !/^(?:[a-z+]+:)?\/\//.test(href)) {
                link.href = path_to_root + href;
            }
            // The 'index' page is supposed to alias the first chapter in the book.
            // Check both with and without the '.html' suffix to be robust against pretty URLs
            if (link.href.replace(/\.html$/, '') === current_page.replace(/\.html$/, '')
                || i === 0
                && path_to_root === ''
                && current_page.endsWith('/index.html')) {
                link.classList.add('active');
                let parent = link.parentElement;
                while (parent) {
                    if (parent.tagName === 'LI' && parent.classList.contains('chapter-item')) {
                        parent.classList.add('expanded');
                    }
                    parent = parent.parentElement;
                }
            }
        }
        // Track and set sidebar scroll position
        this.addEventListener('click', e => {
            if (e.target.tagName === 'A') {
                const clientRect = e.target.getBoundingClientRect();
                const sidebarRect = this.getBoundingClientRect();
                sessionStorage.setItem('sidebar-scroll-offset', clientRect.top - sidebarRect.top);
            }
        }, { passive: true });
        const sidebarScrollOffset = sessionStorage.getItem('sidebar-scroll-offset');
        sessionStorage.removeItem('sidebar-scroll-offset');
        if (sidebarScrollOffset !== null) {
            // preserve sidebar scroll position when navigating via links within sidebar
            const activeSection = this.querySelector('.active');
            if (activeSection) {
                const clientRect = activeSection.getBoundingClientRect();
                const sidebarRect = this.getBoundingClientRect();
                const currentOffset = clientRect.top - sidebarRect.top;
                this.scrollTop += currentOffset - parseFloat(sidebarScrollOffset);
            }
        } else {
            // scroll sidebar to current active section when navigating via
            // 'next/previous chapter' buttons
            const activeSection = document.querySelector('#mdbook-sidebar .active');
            if (activeSection) {
                activeSection.scrollIntoView({ block: 'center' });
            }
        }
        // Toggle buttons
        const sidebarAnchorToggles = document.querySelectorAll('.chapter-fold-toggle');
        function toggleSection(ev) {
            ev.currentTarget.parentElement.parentElement.classList.toggle('expanded');
        }
        Array.from(sidebarAnchorToggles).forEach(el => {
            el.addEventListener('click', toggleSection);
        });
    }
}
window.customElements.define('mdbook-sidebar-scrollbox', MDBookSidebarScrollbox);

