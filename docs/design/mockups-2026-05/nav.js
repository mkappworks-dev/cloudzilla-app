/* Common navbar -- single source of truth for the repo top bar + section subnav.
   Each page uses:  <div id="cz-header" data-repo-tab="issues"></div>
                    <script src="nav.js"></script>
   data-repo-tab: code issues milestones pulls actions discussions projects
   wiki releases settings  (empty for no active tab). Runs synchronously before
   the page's own theme + dropdown scripts, which then wire the injected markup.
   Markup below is a fixed build-time template -- no runtime/user input. */
(function () {
  var mount = document.getElementById('cz-header');
  if (!mount) return;
  var active = mount.getAttribute('data-repo-tab') || '';

  var TOPBAR = `
  <div class="max-w-[1400px] mx-auto px-6 h-12 flex items-center gap-3">
    <a href="home.html" class="flex items-center" aria-label="Cloudzilla, home">
      <svg width="22" height="22" viewBox="0 0 64 64" fill="currentColor"><circle cx="22" cy="36" r="9"/><circle cx="32" cy="28" r="11"/><circle cx="44" cy="36" r="9"/><rect x="22" y="36" width="22" height="9" rx="4.5"/></svg>
    </a>
    <svg class="fg3" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1" aria-hidden="true" focusable="false"><path d="M16 4L8 20"/></svg>
    <div class="dropdown-wrap">
      <button type="button" class="flex items-center gap-2 -ml-1 px-2 h-7 rounded-md hover:elevated text-sm" aria-haspopup="menu" aria-expanded="false" data-menu aria-label="Switch account or organization, current: mkappworks">
      <span class="avatar h-5 w-5 rounded grid place-items-center text-[10px] font-medium" aria-hidden="true">M</span>
      <span class="font-medium">mkappworks</span>
      <svg class="fg3" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" aria-hidden="true" focusable="false"><path d="M7 10l5 5 5-5"/></svg>
    </button>
      <div class="dropdown-panel hidden" role="menu" aria-label="Switch account or organization">
        <p class="dropdown-header">Account / orgs</p>
        <button type="button" role="menuitem" class="dropdown-item dropdown-item-active">
          <span class="avatar h-5 w-5 rounded grid place-items-center text-[10px] font-medium" aria-hidden="true">M</span>
          <span class="flex-1">mkappworks</span>
          <span class="text-[10px] fg3">Personal</span>
          <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><path d="M20 6L9 17l-5-5"/></svg>
        </button>
        <button type="button" role="menuitem" class="dropdown-item">
          <span class="avatar h-5 w-5 rounded grid place-items-center text-[10px] font-medium" aria-hidden="true">A</span>
          <span class="flex-1">acme-corp</span>
          <span class="text-[10px] fg3">Member</span>
        </button>
        <button type="button" role="menuitem" class="dropdown-item">
          <span class="avatar h-5 w-5 rounded grid place-items-center text-[10px] font-medium" aria-hidden="true">S</span>
          <span class="flex-1">stitch-labs</span>
          <span class="text-[10px] fg3">Owner</span>
        </button>
        <button type="button" role="menuitem" class="dropdown-item">
          <span class="avatar h-5 w-5 rounded grid place-items-center text-[10px] font-medium" aria-hidden="true">O</span>
          <span class="flex-1">openforge</span>
          <span class="text-[10px] fg3">Collaborator</span>
        </button>
        <div class="dropdown-divider"></div>
        <a href="organizations.html" role="menuitem" class="dropdown-item">
          <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" class="fg3" aria-hidden="true"><path d="M3 21h18M3 7h18M5 21V7l7-4 7 4v14"/></svg>
          <span>Your organizations</span>
        </a>
      </div>
    </div>
    <svg class="fg3" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1" aria-hidden="true" focusable="false"><path d="M16 4L8 20"/></svg>
    <div class="dropdown-wrap">
      <button type="button" class="flex items-center gap-2 -ml-1 px-2 h-7 rounded-md hover:elevated text-sm" aria-haspopup="menu" aria-expanded="false" data-menu aria-label="Switch repository, current: cloudzilla">
        <span class="font-medium">cloudzilla</span>
        <svg class="fg3" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" aria-hidden="true" focusable="false"><path d="M7 10l5 5 5-5"/></svg>
      </button>
      <div class="dropdown-panel hidden" role="menu" aria-label="Switch repository">
        <p class="dropdown-header">mkappworks repositories</p>
        <a href="repo.html" role="menuitem" class="dropdown-item dropdown-item-active">
          <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" class="fg3" aria-hidden="true"><path d="M4 19.5A2.5 2.5 0 016.5 17H20"/><path d="M6.5 2H20v20H6.5A2.5 2.5 0 014 19.5v-15A2.5 2.5 0 016.5 2z"/></svg>
          <span class="flex-1">cloudzilla</span>
          <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><path d="M20 6L9 17l-5-5"/></svg>
        </a>
        <a href="#" role="menuitem" class="dropdown-item">
          <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" class="fg3" aria-hidden="true"><path d="M4 19.5A2.5 2.5 0 016.5 17H20"/><path d="M6.5 2H20v20H6.5A2.5 2.5 0 014 19.5v-15A2.5 2.5 0 016.5 2z"/></svg>
          <span class="flex-1">benchlabs-site</span>
        </a>
        <a href="#" role="menuitem" class="dropdown-item">
          <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" class="fg3" aria-hidden="true"><path d="M4 19.5A2.5 2.5 0 016.5 17H20"/><path d="M6.5 2H20v20H6.5A2.5 2.5 0 014 19.5v-15A2.5 2.5 0 016.5 2z"/></svg>
          <span class="flex-1">go-service-starter</span>
        </a>
        <a href="#" role="menuitem" class="dropdown-item">
          <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" class="fg3" aria-hidden="true"><path d="M4 19.5A2.5 2.5 0 016.5 17H20"/><path d="M6.5 2H20v20H6.5A2.5 2.5 0 014 19.5v-15A2.5 2.5 0 016.5 2z"/></svg>
          <span class="flex-1">ingest-pipeline</span>
        </a>
        <a href="#" role="menuitem" class="dropdown-item">
          <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" class="fg3" aria-hidden="true"><path d="M4 19.5A2.5 2.5 0 016.5 17H20"/><path d="M6.5 2H20v20H6.5A2.5 2.5 0 014 19.5v-15A2.5 2.5 0 016.5 2z"/></svg>
          <span class="flex-1">dotfiles</span>
        </a>
        <div class="dropdown-divider"></div>
        <a href="my_repositories.html" role="menuitem" class="dropdown-item">
          <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" class="fg3" aria-hidden="true"><path d="M8 6h13M8 12h13M8 18h13M3 6h.01M3 12h.01M3 18h.01"/></svg>
          <span>All repositories</span>
        </a>
      </div>
    </div>
    <span class="ml-1 inline-flex items-center text-[10px] px-1.5 py-0.5 elevated b border rounded fg2">Public</span>

    <nav aria-label="Account" class="ml-auto flex items-center gap-1">
      <form action="#" method="GET" class="hidden md:block">
        <label for="topnav-search" class="sr-only">Search Cloudzilla</label>
        <div class="relative">
          <input id="topnav-search" name="q" type="search" placeholder="Search…" class="w-56 h-7 pl-7 pr-10 text-[13px] surface b border rounded-md placeholder:fg3 focus:bs focus:bg-transparent" />
          <svg class="absolute left-2 top-1.5 fg3" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><circle cx="11" cy="11" r="7"/><path d="M21 21l-4.3-4.3"/></svg>
          <kbd class="mono absolute right-1.5 top-1/2 -translate-y-1/2 text-[10px] leading-none px-1.5 py-1 elevated b border rounded fg3" aria-hidden="true">⌘K</kbd>
        </div>
      </form>
      <a href="#" class="relative h-7 w-7 grid place-items-center fg2 hover:text-current rounded-md" aria-label="Notifications (3 unread)">
        <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" aria-hidden="true" focusable="false"><path d="M6 8a6 6 0 0112 0c0 7 3 9 3 9H3s3-2 3-9"/><path d="M10.3 21a1.94 1.94 0 003.4 0"/></svg>
        <span class="absolute top-1 right-1 min-w-[14px] h-[14px] px-1 grid place-items-center text-[9px] font-medium rounded-full leading-none" style="background:var(--danger);color:#fff;box-shadow:0 0 0 2px var(--bg);">3</span>
      </a>
      <button type="button" id="theme-toggle" onclick="toggleTheme()" class="text-sm fg2 hover:text-current px-2.5 h-7 inline-flex items-center gap-1.5 b border rounded-md" aria-pressed="false" aria-label="Switch to light theme">
        <svg class="dark-only" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true" focusable="false"><path d="M21 12.79A9 9 0 1111.21 3 7 7 0 0021 12.79z"/></svg>
        <svg class="light-only hidden" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true" focusable="false"><circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M4.93 4.93l1.41 1.41M17.66 17.66l1.41 1.41M2 12h2M20 12h2M4.93 19.07l1.41-1.41M17.66 6.34l1.41-1.41"/></svg>
      </button>
      <div class="dropdown-wrap">
        <button type="button" class="avatar h-7 w-7 rounded-full grid place-items-center text-xs font-medium ml-1" aria-haspopup="menu" aria-expanded="false" data-menu aria-label="User menu for Malith Kuruppu">MK</button>
      <div class="dropdown-panel dropdown-right hidden" role="menu" aria-label="User menu">
        <div class="px-2.5 py-2">
          <p class="text-[12px] fg3">Signed in as</p>
          <p class="text-[13px] font-medium">malith</p>
        </div>
        <div class="dropdown-divider"></div>
        <a href="#" role="menuitem" class="dropdown-item">
          <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" class="fg3" aria-hidden="true"><path d="M20 21v-2a4 4 0 00-4-4H8a4 4 0 00-4 4v2"/><circle cx="12" cy="7" r="4"/></svg>
          Your profile
        </a>
        <a href="stars.html" role="menuitem" class="dropdown-item">
          <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" class="fg3" aria-hidden="true"><polygon points="12 2 15.09 8.26 22 9.27 17 14.14 18.18 21.02 12 17.77 5.82 21.02 7 14.14 2 9.27 8.91 8.26 12 2"/></svg>
          Your stars
        </a>
        <div class="dropdown-divider"></div>
        <a href="account_settings.html" role="menuitem" class="dropdown-item">
          <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" class="fg3" aria-hidden="true"><circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.65 1.65 0 00.33 1.82l.06.06a2 2 0 01-2.83 2.83l-.06-.06a1.65 1.65 0 00-1.82-.33 1.65 1.65 0 00-1 1.51V21a2 2 0 01-4 0v-.09A1.65 1.65 0 009 19.4a1.65 1.65 0 00-1.82.33l-.06.06a2 2 0 01-2.83-2.83l.06-.06a1.65 1.65 0 00.33-1.82 1.65 1.65 0 00-1.51-1H3a2 2 0 010-4h.09A1.65 1.65 0 004.6 9a1.65 1.65 0 00-.33-1.82l-.06-.06a2 2 0 012.83-2.83l.06.06a1.65 1.65 0 001.82.33H9a1.65 1.65 0 001-1.51V3a2 2 0 014 0v.09a1.65 1.65 0 001 1.51 1.65 1.65 0 001.82-.33l.06-.06a2 2 0 012.83 2.83l-.06.06a1.65 1.65 0 00-.33 1.82V9a1.65 1.65 0 001.51 1H21a2 2 0 010 4h-.09a1.65 1.65 0 00-1.51 1z"/></svg>
          Settings
        </a>
        <a href="#" role="menuitem" class="dropdown-item">
          <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" class="fg3" aria-hidden="true"><path d="M2 3h6a4 4 0 014 4v14a3 3 0 00-3-3H2z"/></svg>
          Docs
        </a>
        <a href="#" role="menuitem" class="dropdown-item">
          <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" class="fg3" aria-hidden="true"><circle cx="12" cy="12" r="10"/><path d="M12 6v6l4 2"/></svg>
          Changelog
        </a>
        <div class="dropdown-divider"></div>
        <a href="auth.html" role="menuitem" class="dropdown-item" style="color:var(--danger)">
          <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" aria-hidden="true"><path d="M9 21H5a2 2 0 01-2-2V5a2 2 0 012-2h4"/><polyline points="16 17 21 12 16 7"/><line x1="21" y1="12" x2="9" y2="12"/></svg>
          Sign out
        </a>
      </div>
      </div>
    </nav>
  </div>`;

  var ITEMS = [
    { tab: 'code',        href: 'repo.html',       label: 'Code',          icon: '<polyline points="16 18 22 12 16 6"/><polyline points="8 6 2 12 8 18"/>' },
    { tab: 'pulls',       href: 'pulls.html',      label: 'Pull requests', count: '12', countLabel: '12 open',     icon: '<circle cx="6" cy="6" r="2"/><circle cx="6" cy="18" r="2"/><circle cx="18" cy="18" r="2"/><path d="M6 8v8M6 18h6a6 6 0 006-6v-2"/>' },
    { tab: 'issues',      href: 'issues.html',     label: 'Issues',        count: '23', countLabel: '23 open',     icon: '<circle cx="12" cy="12" r="9"/><circle cx="12" cy="12" r="3"/>' },
    { tab: 'milestones',  href: 'milestones.html', label: 'Milestones',    icon: '<path d="M4 15s1-1 4-1 5 2 8 2 4-1 4-1V3s-1 1-4 1-5-2-8-2-4 1-4 1z"/><line x1="4" y1="22" x2="4" y2="15"/>' },
    { tab: 'actions',     href: 'actions.html',    label: 'Actions',       icon: '<polygon points="5 3 19 12 5 21 5 3"/>' },
    { tab: 'discussions', href: 'discussions.html',label: 'Discussions',   count: '5',  countLabel: '5 threads',   icon: '<path d="M21 11.5a8.38 8.38 0 01-.9 3.8 8.5 8.5 0 01-7.6 4.7 8.38 8.38 0 01-3.8-.9L3 21l1.9-5.7a8.38 8.38 0 01-.9-3.8 8.5 8.5 0 014.7-7.6 8.38 8.38 0 013.8-.9h.5a8.48 8.48 0 018 8v.5z"/>' },
    { tab: 'projects',    href: 'project.html',    label: 'Projects',      icon: '<rect x="3" y="3" width="7" height="9" rx="1"/><rect x="14" y="3" width="7" height="5" rx="1"/><rect x="14" y="12" width="7" height="9" rx="1"/><rect x="3" y="16" width="7" height="5" rx="1"/>' },
    { tab: 'wiki',        href: 'wiki.html',       label: 'Wiki',          icon: '<path d="M4 19.5A2.5 2.5 0 016.5 17H20"/><path d="M6.5 2H20v20H6.5A2.5 2.5 0 014 19.5v-15A2.5 2.5 0 016.5 2z"/>' },
    { tab: 'releases',    href: 'releases.html',   label: 'Releases',      count: '3',  countLabel: '3 published', icon: '<path d="M20.59 13.41l-7.17 7.17a2 2 0 01-2.83 0L2 12V2h10l8.59 8.59a2 2 0 010 2.82z"/><line x1="7" y1="7" x2="7.01" y2="7"/>' },
    { tab: 'settings',    href: 'settings.html',   label: 'Settings',      right: true, icon: '<circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.65 1.65 0 00.33 1.82l.06.06a2 2 0 01-2.83 2.83l-.06-.06a1.65 1.65 0 00-1.82-.33 1.65 1.65 0 00-1 1.51V21a2 2 0 01-4 0v-.09A1.65 1.65 0 009 19.4a1.65 1.65 0 00-1.82.33l-.06.06a2 2 0 01-2.83-2.83l.06-.06a1.65 1.65 0 00.33-1.82 1.65 1.65 0 00-1.51-1H3a2 2 0 010-4h.09A1.65 1.65 0 004.6 9a1.65 1.65 0 00-.33-1.82l-.06-.06a2 2 0 012.83-2.83l.06.06a1.65 1.65 0 001.82.33H9a1.65 1.65 0 001-1.51V3a2 2 0 014 0v.09a1.65 1.65 0 001 1.51 1.65 1.65 0 001.82-.33l.06-.06a2 2 0 012.83 2.83l-.06.06a1.65 1.65 0 00-.33 1.82V9a1.65 1.65 0 001.51 1H21a2 2 0 010 4h-.09a1.65 1.65 0 00-1.51 1z"/>' }
  ];

  function link(it) {
    var on = it.tab === active;
    var cls = (it.right ? 'ml-auto ' : '') +
      'px-3 py-2.5 border-b border-transparent ' +
      (on ? 'tab-active font-medium' : 'fg2 hover:text-current') +
      ' inline-flex items-center gap-1.5';
    var count = it.count
      ? ' <span class="mono text-[11px] fg3 ml-0.5" aria-label="' + it.countLabel + '">' + it.count + '</span>'
      : '';
    return '<a href="' + it.href + '" class="' + cls + '"' + (on ? ' aria-current="page"' : '') + '>' +
      '<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" ' +
      'stroke-width="1.75" aria-hidden="true" focusable="false">' + it.icon + '</svg> ' +
      it.label + count + '</a>';
  }

  var subnav = '<nav class="max-w-[1400px] mx-auto px-6 -mb-px flex items-center gap-1 ' +
    'text-sm overflow-x-auto" aria-label="Repository sections">' +
    ITEMS.map(link).join('') + '</nav>';

  var markup = '<header class="b border-b" role="banner">' + TOPBAR + subnav + '</header>';
  var parsed = new DOMParser().parseFromString(markup, 'text/html').body.firstChild;
  mount.replaceWith(parsed);
})();
