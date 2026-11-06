/* App shell for the mockups, mirroring internal/view/layout/layout.templ.
 *
 *   <div id="cz-header" data-repo-tab="issues"></div>      repo pages (code pull_requests issues milestones checks
 *                                                          discussions projects wiki releases settings)
 *   <div id="cz-header" data-account-tab="repositories">   signed-in account pages (overview repositories gists
 *                                                          pulls issues attention activity settings)
 *   <div id="cz-header"></div>                             no subnav
 *   <div id="cz-header" data-anon></div>                   signed-out header
 *   <div id="cz-footer"></div>                             optional; renders the layout footer
 *
 * All markup is a fixed build-time template; nothing here takes runtime or user input. */
(function () {
  // An instance serves no docs of its own, so layout.templ links to the upstream repo; so do the mocks.
  var REPO_URL = 'https://github.com/mkappworks-dev/cloudzilla-app/';
  var DOCS_URL = REPO_URL + 'tree/main/docs';
  var API_URL = REPO_URL + 'blob/main/docs/api-reference.md';
  var CHANGELOG_URL = REPO_URL + 'blob/main/CHANGELOG.md';

  var ICON = {
    code: '<polyline points="16 18 22 12 16 6"/><polyline points="8 6 2 12 8 18"/>',
    pull_requests: '<circle cx="6" cy="6" r="2"/><circle cx="6" cy="18" r="2"/><circle cx="18" cy="18" r="2"/><path d="M6 8v8M6 18h6a6 6 0 006-6v-2"/>',
    issues: '<circle cx="12" cy="12" r="9"/><circle cx="12" cy="12" r="3"/>',
    milestones: '<path d="M4 15s1-1 4-1 5 2 8 2 4-1 4-1V3s-1 1-4 1-5-2-8-2-4 1-4 1z"/><line x1="4" y1="22" x2="4" y2="15"/>',
    checks: '<circle cx="12" cy="12" r="9"/><polygon points="10 8 16 12 10 16 10 8"/>',
    discussions: '<path d="M21 11.5a8.38 8.38 0 01-.9 3.8 8.5 8.5 0 01-7.6 4.7 8.38 8.38 0 01-3.8-.9L3 21l1.9-5.7a8.38 8.38 0 01-.9-3.8 8.5 8.5 0 014.7-7.6 8.38 8.38 0 013.8-.9h.5a8.48 8.48 0 018 8v.5z"/>',
    projects: '<rect x="3" y="3" width="7" height="9" rx="1"/><rect x="14" y="3" width="7" height="5" rx="1"/><rect x="14" y="12" width="7" height="9" rx="1"/><rect x="3" y="16" width="7" height="5" rx="1"/>',
    wiki: '<path d="M4 19.5A2.5 2.5 0 016.5 17H20"/><path d="M6.5 2H20v20H6.5A2.5 2.5 0 014 19.5v-15A2.5 2.5 0 016.5 2z"/>',
    releases: '<path d="M20.59 13.41l-7.17 7.17a2 2 0 01-2.83 0L2 12V2h10l8.59 8.59a2 2 0 010 2.82z"/><line x1="7" y1="7" x2="7.01" y2="7"/>',
    settings: '<circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.65 1.65 0 00.33 1.82l.06.06a2 2 0 01-2.83 2.83l-.06-.06a1.65 1.65 0 00-1.82-.33 1.65 1.65 0 00-1 1.51V21a2 2 0 01-4 0v-.09A1.65 1.65 0 009 19.4a1.65 1.65 0 00-1.82.33l-.06.06a2 2 0 01-2.83-2.83l.06-.06a1.65 1.65 0 00.33-1.82 1.65 1.65 0 00-1.51-1H3a2 2 0 010-4h.09A1.65 1.65 0 004.6 9a1.65 1.65 0 00-.33-1.82l-.06-.06a2 2 0 012.83-2.83l.06.06a1.65 1.65 0 001.82.33H9a1.65 1.65 0 001-1.51V3a2 2 0 014 0v.09a1.65 1.65 0 001 1.51 1.65 1.65 0 001.82-.33l.06-.06a2 2 0 012.83 2.83l-.06.06a1.65 1.65 0 00-.33 1.82V9a1.65 1.65 0 001.51 1H21a2 2 0 010 4h-.09a1.65 1.65 0 00-1.51 1z"/>',
    user: '<path d="M20 21v-2a4 4 0 00-4-4H8a4 4 0 00-4 4v2"/><circle cx="12" cy="7" r="4"/>',
    star: '<polygon points="12 2 15.09 8.26 22 9.27 17 14.14 18.18 21.02 12 17.77 5.82 21.02 7 14.14 2 9.27 8.91 8.26 12 2"/>',
    shield: '<path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/>',
    book: '<path d="M2 3h6a4 4 0 014 4v14a3 3 0 00-3-3H2z"/>',
    clock: '<circle cx="12" cy="12" r="10"/><path d="M12 6v6l4 2"/>',
    logout: '<path d="M9 21H5a2 2 0 01-2-2V5a2 2 0 012-2h4"/><polyline points="16 17 21 12 16 7"/><line x1="21" y1="12" x2="9" y2="12"/>',
    plus: '<path d="M12 5v14M5 12h14"/>',
    org: '<path d="M3 21h18M3 7h18M5 21V7l7-4 7 4v14"/>',
  };

  function svg(name, size, extra) {
    return '<svg width="' + size + '" height="' + size + '" viewBox="0 0 24 24" fill="none" stroke="currentColor" ' +
      'stroke-width="1.75" aria-hidden="true" focusable="false"' + (extra || '') + '>' + ICON[name] + '</svg>';
  }

  var SEP = '<svg class="text-muted-foreground" width="14" height="14" viewBox="0 0 24 24" fill="none" ' +
    'stroke="currentColor" stroke-width="1" aria-hidden="true" focusable="false"><path d="M16 4L8 20"/></svg>';
  var CHEVRON = '<svg class="text-muted-foreground" width="12" height="12" viewBox="0 0 24 24" fill="none" ' +
    'stroke="currentColor" stroke-width="1.5" aria-hidden="true" focusable="false"><path d="M7 10l5 5 5-5"/></svg>';

  var ITEM = 'flex items-center gap-2 px-2 py-1.5 rounded-xs hover:bg-accent hover:text-accent-foreground ' +
    'focus:bg-accent focus:text-accent-foreground focus:outline-hidden whitespace-nowrap';
  var PANEL = 'hidden absolute top-full mt-1.5 z-50 min-w-[12rem] rounded-md border border-border bg-popover ' +
    'text-popover-foreground shadow-md p-1 text-sm';

  function chip(letter) {
    return '<span class="relative inline-grid place-items-center h-5 w-5 shrink-0 overflow-hidden rounded-sm ' +
      'bg-accent border border-border text-[10px] font-medium" aria-hidden="true">' + letter + '</span>';
  }
  function label(text) {
    return '<div class="px-2 py-1.5 text-[11px] uppercase tracking-wider text-muted-foreground">' + text + '</div>';
  }
  var DIVIDER = '<div role="separator" class="my-1 h-px bg-border"></div>';
  function link(href, inner, cls) {
    return '<a href="' + href + '" role="menuitem" class="' + ITEM + (cls ? ' ' + cls : '') + '">' + inner + '</a>';
  }
  function menu(trigger, triggerCls, aria, align, body) {
    return '<div class="relative inline-block" data-shell-menu>' +
      '<button type="button" class="' + triggerCls + '" aria-haspopup="menu" aria-expanded="false" ' +
      'aria-label="' + aria + '">' + trigger + '</button>' +
      '<div role="menu" class="' + PANEL + ' ' + (align === 'end' ? 'right-0' : 'left-0') + '">' + body + '</div></div>';
  }

  var mount = document.getElementById('cz-header');
  var footerMount = document.getElementById('cz-footer');
  var ds = mount ? mount.dataset : {};
  var anon = mount && mount.hasAttribute('data-anon');
  var repoTab = ds.repoTab || '';
  var accountTab = ds.accountTab || '';
  var owner = ds.owner || 'mkappworks';
  var repo = ds.repo || 'cloudzilla';

  var workspaceBlock = '';
  if (!anon) {
    workspaceBlock = SEP + menu(
      chip('M') + '<span class="font-medium">' + owner + '</span>' + CHEVRON,
      'flex items-center gap-2 -ml-1 px-2 h-7 rounded-md hover:bg-accent text-sm font-medium',
      'Switch account or organization, current: ' + owner, 'start',
      label('Account / orgs') +
      link('profile.html', chip('M') + '<span class="flex-1 truncate max-w-64">' + owner + '</span>' +
        '<span class="text-[10px] text-muted-foreground">Personal</span>') +
      link('org_profile.html', chip('A') + '<span class="flex-1 truncate max-w-64">acme-corp</span>' +
        '<span class="text-[10px] text-muted-foreground">Member</span>') +
      link('org_profile.html', chip('S') + '<span class="flex-1 truncate max-w-64">stitch-labs</span>' +
        '<span class="text-[10px] text-muted-foreground">Owner</span>') +
      DIVIDER +
      link('new_organization.html', svg('plus', 13, ' class="text-muted-foreground"') + 'New organization') +
      link('organizations.html', svg('org', 13, ' class="text-muted-foreground"') + 'Your organizations'));
  }
  var repoBlock = '';
  if (!anon && repoTab) {
    repoBlock = SEP + menu(
      '<span class="font-medium">' + repo + '</span>' +
      '<svg class="text-muted-foreground" width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" ' +
      'stroke-width="2" aria-hidden="true" focusable="false"><path d="M6 9l6 6 6-6"/></svg>',
      'flex items-center gap-2 -ml-1 px-2 h-7 rounded-md hover:bg-accent text-sm',
      'Switch repository, current: ' + owner + '/' + repo, 'start',
      label(owner + ' repositories') +
      link('repo.html', '<span class="truncate max-w-64">cloudzilla</span>') +
      link('repo.html', '<span class="truncate max-w-64">benchlabs-site</span>') +
      link('repo.html', '<span class="truncate max-w-64">go-service-starter</span>') +
      DIVIDER +
      link('my_repositories.html', 'All repositories')) +
      '<span class="inline-flex items-center text-[10px] px-1.5 py-0.5 bg-muted border border-border rounded-sm ' +
      'text-muted-foreground">Public</span>';
  }

  var search =
    '<form action="search.html" method="GET" class="hidden md:block" data-cz-search><label for="topnav-search" class="sr-only">Search Cloudzilla</label>' +
    '<div class="relative"><input id="topnav-search" name="q" type="search" placeholder="Search…" autocomplete="off" role="combobox" ' +
    'aria-autocomplete="list" aria-haspopup="listbox" aria-controls="topnav-suggest" aria-expanded="false" ' +
    'class="w-72 focus:w-96 transition-[width] duration-150 h-7 pl-7 pr-14 text-[13px] bg-muted border border-transparent ' +
    'hover:border-border focus:bg-background focus:border-border rounded-md placeholder:text-muted-foreground" />' +
    '<svg class="absolute left-2 top-1.5 text-muted-foreground" width="14" height="14" viewBox="0 0 24 24" fill="none" ' +
    'stroke="currentColor" stroke-width="2" aria-hidden="true" focusable="false"><circle cx="11" cy="11" r="7"/><path d="M21 21l-4.3-4.3"/></svg>' +
    '<button type="button" data-cz-clear class="hidden absolute right-1.5 top-1 h-5 w-5 grid place-items-center rounded-full text-muted-foreground hover:text-foreground hover:bg-accent" aria-label="Clear search">' +
    '<svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.25" stroke-linecap="round" aria-hidden="true" focusable="false"><path d="M18 6L6 18M6 6l12 12"/></svg></button>' +
    '<kbd data-cz-kbd class="absolute right-1.5 top-1 h-5 min-w-5 px-1 grid place-items-center text-[11px] font-sans leading-none ' +
    'text-muted-foreground border border-border rounded" aria-hidden="true">/</kbd>' +
    '<div id="topnav-suggest" class="hidden absolute inset-x-0 top-full mt-1 z-50 overflow-hidden rounded-md border border-border bg-popover text-popover-foreground shadow-lg"></div></div></form>';

  var bell = '<a href="notifications.html" class="relative h-7 w-7 grid place-items-center text-muted-foreground ' +
    'hover:text-foreground rounded-md hover:bg-accent" aria-label="Notifications, 1492 unread">' +
    '<svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" aria-hidden="true" focusable="false">' +
    '<path d="M6 8a6 6 0 0112 0c0 7 3 9 3 9H3s3-2 3-9"/><path d="M10.3 21a1.94 1.94 0 003.4 0"/></svg>' +
    '<span class="absolute -top-1 -right-1 min-w-[14px] h-[14px] px-1 grid place-items-center bg-destructive ' +
    'text-destructive-foreground text-[9px] font-medium rounded-full leading-none ring-2 ring-background">99+</span></a>';

  var themeToggle = '<button type="button" id="theme-toggle" class="text-muted-foreground hover:text-foreground h-7 w-7 ' +
    'grid place-items-center rounded-md border border-border hover:bg-accent" aria-pressed="false" aria-label="Switch to light theme">' +
    '<svg class="dark-only" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true" focusable="false"><path d="M21 12.79A9 9 0 1111.21 3 7 7 0 0021 12.79z"/></svg>' +
    '<svg class="light-only hidden" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true" focusable="false"><circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M4.93 4.93l1.41 1.41M17.66 17.66l1.41 1.41M2 12h2M20 12h2M4.93 19.07l1.41-1.41M17.66 6.34l1.41-1.41"/></svg></button>';

  var userMenu = '<div class="ml-1">' + menu(
    '<span class="inline-grid place-items-center rounded-full bg-muted border border-border font-medium text-muted-foreground h-7 w-7 text-xs" aria-hidden="true">' +
      escHtml(initialsOf(owner)) + '</span>',
    'grid rounded-full [&:hover>span]:border-foreground/30',
    'User menu for ' + owner, 'end',
    '<div class="px-2.5 py-2"><p class="text-[12px] text-muted-foreground">Signed in as</p>' +
    '<p class="text-[13px] font-medium">' + owner + '</p></div>' + DIVIDER +
    link('profile.html', svg('user', 13, ' class="text-muted-foreground"') + 'Your profile') +
    link('stars.html', svg('star', 13, ' class="text-muted-foreground"') + 'Your stars') + DIVIDER +
    link('account_settings.html', svg('settings', 13, ' class="text-muted-foreground"') + 'Settings') +
    link('admin_settings.html', svg('shield', 13, ' class="text-muted-foreground"') + 'Admin') +
    link(DOCS_URL, svg('book', 13, ' class="text-muted-foreground"') + 'Docs') +
    link(CHANGELOG_URL, svg('clock', 13, ' class="text-muted-foreground"') + 'Changelog') + DIVIDER +
    link('login.html', svg('logout', 13, ' class="text-destructive"') + '<span class="text-destructive">Sign out</span>')) + '</div>';

  var anonLinks =
    '<a href="login.html" class="text-sm text-muted-foreground hover:text-foreground px-2.5 h-7 inline-flex items-center">Sign in</a>' +
    '<a href="register.html" class="text-sm font-medium h-7 px-3 inline-flex items-center bg-primary text-primary-foreground rounded-md hover:bg-primary/90">Register</a>';

  function tab(key, text, href, active, extra) {
    var base = 'px-3 py-1.5 rounded-md inline-flex items-center gap-1.5 ' + (extra || '');
    return active
      ? '<a href="' + href + '" aria-current="page" class="' + base + ' bg-accent font-medium text-foreground">' + text + '</a>'
      : '<a href="' + href + '" class="' + base + ' text-muted-foreground hover:text-foreground hover:bg-accent">' + text + '</a>';
  }
  var SUBNAV_CLS = 'max-w-[1400px] mx-auto px-6 py-1.5 flex items-center gap-1 text-sm overflow-x-auto ' +
    '[scrollbar-width:none] [&::-webkit-scrollbar]:hidden';
  function count(n) {
    return n ? ' <span class="font-mono text-[11px] text-muted-foreground ml-0.5">' + n + '</span>' : '';
  }

  var REPO_TABS = [
    ['code', 'Code', 'repo.html', 0], ['pull_requests', 'Pull requests', 'pulls.html', 12],
    ['issues', 'Issues', 'issues.html', 23], ['milestones', 'Milestones', 'milestones.html', 0],
    ['checks', 'Checks', 'actions.html', 0], ['discussions', 'Discussions', 'discussions.html', 5],
    ['projects', 'Projects', 'project.html', 0], ['wiki', 'Wiki', 'wiki.html', 0],
    ['releases', 'Releases', 'releases.html', 3], ['settings', 'Settings', 'settings.html', 0, 'ml-auto'],
  ];
  var ACCOUNT_TABS = [
    ['overview', 'Overview', 'home.html'], ['repositories', 'Repositories', 'my_repositories.html'],
    ['gists', 'Gists', 'gists.html'], ['pulls', 'Pull requests', 'my_pulls.html'],
    ['issues', 'Issues', 'my_issues.html'], ['attention', 'Attention', 'attention.html'],
    ['activity', 'Activity', 'activity.html'], ['settings', 'Settings', 'account_settings.html'],
  ];

  var subnav = '';
  if (repoTab) {
    subnav = '<nav aria-label="Repository sections" class="' + SUBNAV_CLS + '">' + REPO_TABS.map(function (t) {
      return tab(t[0], svg(t[0], 14) + ' ' + t[1] + count(t[3]), t[2], t[0] === repoTab, t[4]);
    }).join('') + '</nav>';
  } else if (accountTab) {
    subnav = '<nav aria-label="Primary" class="' + SUBNAV_CLS + '">' + ACCOUNT_TABS.map(function (t) {
      return tab(t[0], t[1], t[2], t[0] === accountTab);
    }).join('') + '</nav>';
  }

  var header =
    '<header role="banner" class="border-b border-border"><div class="max-w-[1400px] mx-auto px-6 h-12 flex items-center gap-3">' +
    '<a href="home.html" class="flex items-center" aria-label="Cloudzilla, home">' +
    '<svg width="22" height="22" viewBox="0 0 64 64" fill="currentColor" aria-hidden="true" focusable="false">' +
    '<circle cx="22" cy="36" r="9"/><circle cx="32" cy="28" r="11"/><circle cx="44" cy="36" r="9"/><rect x="22" y="36" width="22" height="9" rx="4.5"/></svg></a>' +
    workspaceBlock + repoBlock +
    '<nav aria-label="Account" class="ml-auto flex items-center gap-1">' + search + (anon ? '' : bell) + themeToggle +
    (anon ? anonLinks : userMenu) + '</nav></div>' + subnav + '</header>';

  var footer =
    '<footer role="contentinfo" class="mt-20 w-full max-w-[1400px] mx-auto px-6 text-[11px] text-muted-foreground">' +
    '<div class="border-t border-border py-6 flex items-center justify-between"><p class="font-mono">cloudzilla / dev ' +
    '<span aria-hidden="true">·</span> <span aria-hidden="true" class="text-success">●</span> all systems normal</p>' +
    '<nav aria-label="Footer"><ul class="flex gap-4 list-none"><li><a href="' + DOCS_URL + '" class="hover:text-foreground">Docs</a></li>' +
    '<li><a href="' + API_URL + '" class="hover:text-foreground">API</a></li><li><a href="' + CHANGELOG_URL + '" class="hover:text-foreground">Changelog</a></li></ul></nav></div></footer>';

  function parse(html) {
    return new DOMParser().parseFromString(html, 'text/html').body.firstChild;
  }
  if (mount) mount.replaceWith(parse(header));
  if (footerMount) footerMount.replaceWith(parse(footer));

  // Search suggestions, mirroring fragments/search_suggestions.templ: repos, users and orgs whose name contains the
  // typed text (two characters minimum), then a "Search for" row. The real fragment has no issue or code rows.
  var SUGGEST = {
    repo: [
      ['mkappworks/cloudzilla', 'Self-hosted git forge', 'repo.html', false],
      ['mkappworks/benchlabs-site', 'Marketing site for Bench Labs', 'repo.html', false],
      ['mkappworks/go-service-starter', 'Opinionated Go service template', 'repo.html', true],
      ['acme-corp/oauth-proxy', 'Reverse proxy that fronts apps with OAuth 2.0', 'repo.html', false],
      ['stitch-labs/htmx-kit', 'Tiny HTMX component toolkit', 'repo.html', false],
      ['openforge/runner', 'Sandboxed CI runner', 'repo.html', false],
    ],
    user: [['malith', '', 'profile.html'], ['daisy', '', 'profile.html'], ['kiyo', '', 'profile.html'],
      ['rhea', '', 'profile.html'], ['oauth-bot', '', 'profile.html']],
    org: [['acme-corp', 'Acme Corp', 'org_profile.html'], ['stitch-labs', 'Stitch Labs', 'org_profile.html'],
      ['openforge', 'Open Forge', 'org_profile.html']],
  };
  var SUGGEST_LABEL = { repo: 'Repositories', user: 'Users', org: 'Organizations' };
  var BOOK = '<svg class="shrink-0 text-muted-foreground" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true" focusable="false"><path d="M4 19.5A2.5 2.5 0 016.5 17H20"/><path d="M6.5 2H20v20H6.5A2.5 2.5 0 014 19.5v-15A2.5 2.5 0 016.5 2z"/></svg>';
  var SUGGEST_ROW = 'flex items-center gap-2 px-3 py-1.5 text-[13px] hover:bg-accent aria-selected:bg-accent';
  function escHtml(t) {
    var d = document.createElement('div');
    d.textContent = t;
    return d.innerHTML;
  }
  // components.initials: first letters of the first and last words, so one letter for a username.
  function initialsOf(n) {
    var w = n.trim().split(/\s+/);
    return w[0] ? (w.length > 1 ? w[0][0] + w[w.length - 1][0] : w[0][0]).toUpperCase() : '?';
  }

  // Pages with their own search box (search.html) attach the same behaviour: czAttachSuggest(input, panel).
  function attachSuggest(input, panel) {
    var active = -1;
    function isOpen() { return !panel.classList.contains('hidden'); }
    function setOpen(open) {
      panel.classList.toggle('hidden', !open);
      input.setAttribute('aria-expanded', open ? 'true' : 'false');
      if (!open) { active = -1; input.removeAttribute('aria-activedescendant'); }
    }
    function options() { return panel.querySelectorAll('[data-suggest-item]'); }
    function highlight(i) {
      active = i;
      options().forEach(function (a, n) { a.setAttribute('aria-selected', n === i ? 'true' : 'false'); });
      if (i >= 0) input.setAttribute('aria-activedescendant', panel.id + '-' + i);
      else input.removeAttribute('aria-activedescendant');
    }
    function render() {
      var q = input.value.trim();
      if (q.length < 2) { panel.innerHTML = ''; setOpen(false); return; }
      var needle = q.toLowerCase();
      var rows = [];
      ['repo', 'user', 'org'].forEach(function (kind) {
        SUGGEST[kind].forEach(function (r) {
          if (r[0].toLowerCase().indexOf(needle) < 0) return;
          rows.push({ kind: kind, title: r[0], detail: r[1], href: r[2], priv: kind === 'repo' && r[3] });
        });
      });
      var html = '<div role="listbox" aria-label="Search suggestions" class="py-1">';
      rows.forEach(function (r, i) {
        if (i === 0 || rows[i - 1].kind !== r.kind) {
          html += '<div role="presentation" class="px-3 pt-1.5 pb-0.5 text-[11px] uppercase tracking-wide text-muted-foreground">' + SUGGEST_LABEL[r.kind] + '</div>';
        }
        html += '<a id="' + panel.id + '-' + i + '" role="option" aria-selected="false" data-suggest-item href="' + r.href + '" class="' + SUGGEST_ROW + '">' +
          (r.kind === 'repo' ? BOOK : '<span class="inline-grid place-items-center rounded-full bg-muted border border-border font-medium text-muted-foreground h-5 w-5 text-[10px]" aria-hidden="true">' + escHtml(initialsOf(r.title)) + '</span>') +
          '<span class="shrink-0 truncate font-medium max-w-[65%]">' + escHtml(r.title) + '</span>' +
          (r.priv ? '<span class="shrink-0 text-[10px] px-1.5 py-0.5 bg-muted border border-border rounded-sm text-muted-foreground">Private</span>' : '') +
          (r.detail ? '<span class="truncate text-muted-foreground">' + escHtml(r.detail) + '</span>' : '') + '</a>';
      });
      html += '<a id="' + panel.id + '-' + rows.length + '" role="option" aria-selected="false" data-suggest-item href="search.html?q=' + encodeURIComponent(q) +
        '" class="' + SUGGEST_ROW + ' border-t border-border text-muted-foreground">' +
        '<svg class="shrink-0" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true" focusable="false"><circle cx="11" cy="11" r="7"/><path d="M21 21l-4.3-4.3"/></svg>' +
        '<span class="truncate">Search for “' + escHtml(q) + '”</span></a></div>';
      panel.innerHTML = html;
      active = -1;
      setOpen(true);
      document.dispatchEvent(new CustomEvent('cz-suggest-opened', { detail: panel }));
    }
    function move(delta) {
      var n = options().length;
      if (!n) return;
      setOpen(true);
      highlight(active < 0 ? (delta > 0 ? 0 : n - 1) : (active + delta + n) % n);
      options()[active].scrollIntoView({ block: 'nearest' });
    }
    input.addEventListener('input', render);
    input.addEventListener('focus', function () { if (options().length) setOpen(true); });
    input.addEventListener('keydown', function (e) {
      if (e.key === 'ArrowDown') { e.preventDefault(); move(1); }
      else if (e.key === 'ArrowUp') { e.preventDefault(); move(-1); }
      else if (e.key === 'Enter') {
        // Nothing highlighted: Enter submits the form.
        if (isOpen() && active >= 0) { e.preventDefault(); options()[active].click(); }
      } else if (e.key === 'Escape' && isOpen()) { e.preventDefault(); setOpen(false); }
    });
    panel.addEventListener('mouseover', function (e) {
      var a = e.target.closest && e.target.closest('[data-suggest-item]');
      if (a) highlight(Array.prototype.indexOf.call(options(), a));
    });
    document.addEventListener('click', function (e) {
      if (isOpen() && !input.contains(e.target) && !panel.contains(e.target)) setOpen(false);
    });
    // The topnav and the results page each have a box; only one list shows at a time.
    document.addEventListener('cz-suggest-opened', function (e) { if (e.detail !== panel) setOpen(false); });
    return { close: function () { panel.innerHTML = ''; setOpen(false); } };
  }
  window.czAttachSuggest = attachSuggest;

  var searchForm = document.querySelector('form[data-cz-search]');
  if (searchForm) {
    var searchInput = searchForm.querySelector('input[name=q]');
    var clearBtn = searchForm.querySelector('[data-cz-clear]');
    var kbd = searchForm.querySelector('[data-cz-kbd]');
    var topSuggest = attachSuggest(searchInput, document.getElementById('topnav-suggest'));
    var syncClear = function () {
      var has = searchInput.value !== '';
      clearBtn.classList.toggle('hidden', !has);
      kbd.classList.toggle('hidden', has);
    };
    searchInput.addEventListener('input', syncClear);
    clearBtn.addEventListener('click', function () {
      searchInput.value = '';
      topSuggest.close();
      syncClear();
      searchInput.focus();
    });
    // The generic submit handler below would only toast; this form really navigates.
    searchForm.addEventListener('submit', function (e) {
      e.preventDefault();
      var q = searchInput.value.trim();
      location.href = 'search.html' + (q ? '?q=' + encodeURIComponent(q) : '');
    });
    document.addEventListener('keydown', function (e) {
      if (e.key !== '/' || e.metaKey || e.ctrlKey || e.altKey) return;
      var t = e.target;
      if (t.isContentEditable || /^(INPUT|TEXTAREA|SELECT)$/.test(t.tagName)) return;
      if (searchInput.offsetParent === null) return;
      e.preventDefault();
      searchInput.focus();
      searchInput.select();
    });
  }

  // Theme: same storage key and class polarity as the layout's bootstrap (dark by default, `light` opts out).
  var root = document.documentElement;
  var toggle = document.getElementById('theme-toggle');
  try {
    var saved = localStorage.getItem('cz-theme');
    if (saved === 'light' || (!saved && window.matchMedia('(prefers-color-scheme: light)').matches)) {
      root.classList.remove('dark');
      root.classList.add('light');
    }
  } catch (e) { /* storage blocked */ }
  function syncToggle() {
    if (!toggle) return;
    var light = root.classList.contains('light');
    toggle.setAttribute('aria-pressed', light ? 'true' : 'false');
    toggle.setAttribute('aria-label', light ? 'Switch to dark theme' : 'Switch to light theme');
    toggle.querySelector('.dark-only').classList.toggle('hidden', light);
    toggle.querySelector('.light-only').classList.toggle('hidden', !light);
  }
  if (toggle) {
    toggle.addEventListener('click', function () {
      var light = !root.classList.contains('light');
      root.classList.toggle('light', light);
      root.classList.toggle('dark', !light);
      try { localStorage.setItem('cz-theme', light ? 'light' : 'dark'); } catch (e) { /* storage blocked */ }
      syncToggle();
    });
    syncToggle();
  }

  // Dropdowns: `<div data-dropdown><button/><div role="menu" class="hidden"/></div>`, one open at a time; click
  // outside or Escape closes. Pages with their own menu script mark the trigger `data-dropdown-btn`; those are left alone.
  function managed(d) {
    return !d.querySelector('[data-dropdown-btn]') && d.querySelector(':scope > [role=menu]');
  }
  function closeAll(except) {
    document.querySelectorAll('[data-dropdown], [data-shell-menu]').forEach(function (d) {
      var panel = managed(d);
      if (!panel || d === except || panel.classList.contains('hidden')) return;
      panel.classList.add('hidden');
      d.querySelector(':scope > button').setAttribute('aria-expanded', 'false');
    });
  }
  document.addEventListener('click', function (e) {
    var wrap = e.target.closest && e.target.closest('[data-dropdown], [data-shell-menu]');
    if (wrap && !managed(wrap)) wrap = null;
    var onTrigger = wrap && e.target.closest('[data-dropdown] > button, [data-shell-menu] > button');
    closeAll(onTrigger ? wrap : null);
    if (onTrigger) {
      var open = managed(wrap).classList.toggle('hidden') === false;
      onTrigger.setAttribute('aria-expanded', open ? 'true' : 'false');
    }
  });
  document.addEventListener('keydown', function (e) { if (e.key === 'Escape') closeAll(null); });

  // Anything left unwired says so instead of failing silently: a `#` link or a button that changed nothing on the
  // page gets a toast. Editor toolbars and copy buttons act on text or the clipboard, which isn't a DOM change.
  function toast(text) {
    var t = document.createElement('div');
    t.setAttribute('role', 'status');
    t.className = 'fixed bottom-4 right-4 z-[100] max-w-sm rounded-md border border-border bg-popover px-3 py-2 ' +
      'text-sm text-popover-foreground shadow-md';
    t.textContent = text;
    document.body.appendChild(t);
    setTimeout(function () { t.remove(); }, 2600);
  }
  window.czToast = toast;
  // The click listener below runs in the capture phase: a target's own handler would otherwise change the page, and
  // the observer would count it, before the baseline is taken.
  var changes = 0;
  new MutationObserver(function (l) { changes += l.length; }).observe(document.body, {
    subtree: true, attributes: true, childList: true, characterData: true,
  });
  document.addEventListener('click', function (e) {
    var el = e.target.closest && e.target.closest('a[href="#"], button, [role=button]');
    if (!el || el.disabled || el.closest('[data-md-editor], [data-md-action]') || el.hasAttribute('data-md-action')) return;
    if (el.matches('button[type=submit], [type=reset]') || el.closest('form[method=dialog]')) return;
    if (/^(copy|bold|italic|code|insert link)/i.test((el.getAttribute('aria-label') || el.textContent).trim())) return;
    var seen = changes;
    var label = (el.getAttribute('aria-label') || el.textContent || '').replace(/\s+/g, ' ').trim().slice(0, 40);
    setTimeout(function () {
      if (changes === seen) toast((label ? '“' + label + '”' : 'This control') + ' isn’t wired up in the mock.');
    }, 60);
  }, true);
  document.addEventListener('submit', function (e) {
    if (e.defaultPrevented) return;
    e.preventDefault();
    toast('Submitted. The mock stays on this page.');
  });
})();
