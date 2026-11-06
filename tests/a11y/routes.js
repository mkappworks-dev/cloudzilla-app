// The desktop pages of mockups/index.html. {key} is filled from the fixtures
// global-setup.js discovers. Routes that need a one-time token are checked in
// their invalid-link state, which is what a bogus token renders.
export const routes = [
  // Account
  { name: 'home', path: '/' },
  { name: 'activity', path: '/activity' },
  { name: 'attention', path: '/attention' },
  { name: 'notifications', path: '/notifications' },
  { name: 'my repositories', path: '/{me}?tab=repositories' },
  { name: 'my pulls', path: '/pulls' },
  { name: 'my issues', path: '/issues' },
  { name: 'stars', path: '/stars' },
  { name: 'profile', path: '/{user}' },
  { name: 'account settings', path: '/settings' },
  { name: 'repo transfers', path: '/repos/transfers' },
  { name: 'repo import', path: '/repos/import' },
  { name: 'new repo', path: '/repos/new' },

  // Code
  { name: 'repo', path: '{repo}' },
  { name: 'tree', path: '{repo}/tree/main' },
  { name: 'blob', path: '{blob}' },
  { name: 'blame', path: '{blame}' },
  { name: 'commits', path: '{repo}/commits/main' },
  { name: 'commit', path: '{commit}' },
  { name: 'contributors', path: '{repo}/graphs/contributors' },
  { name: 'dependencies', path: '{repo}/network/dependencies' },
  { name: 'pulse', path: '{repo}/pulse' },
  { name: 'repo settings', path: '{ownRepo}/settings' },
  { name: 'refs', path: '{repo}/refs' },
  { name: 'new file', path: '{ownRepo}/new/main' },
  { name: 'edit file', path: '{ownRepo}/edit/main/README.md' },
  { name: 'stargazers', path: '{repo}/stargazers' },
  { name: 'fork', path: '{repo}/fork' },

  // Issues and pull requests
  { name: 'issues', path: '{repo}/issues' },
  { name: 'issues closed', path: '{repo}/issues?state=closed' },
  { name: 'issue', path: '{issue}' },
  { name: 'issue closed', path: '{closedIssue}' },
  { name: 'new issue', path: '{repo}/issues/new' },
  { name: 'milestones', path: '{milestoneRepo}/milestones' },
  { name: 'milestone', path: '{milestone}' },
  { name: 'new milestone', path: '{ownRepo}/milestones/new' },
  { name: 'pulls', path: '{repo}/pulls' },
  { name: 'new pull', path: '{repo}/pulls/new' },
  { name: 'pull', path: '{pull}' },
  { name: 'pull commits', path: '{pull}/commits' },
  { name: 'pull files', path: '{pull}/files' },
  { name: 'pull checks', path: '{pull}/checks' },
  { name: 'checks', path: '{repo}/checks' },

  // Collaboration
  { name: 'discussions', path: '{discussionRepo}/discussions' },
  { name: 'discussion', path: '{discussion}' },
  { name: 'new discussion', path: '{ownRepo}/discussions/new' },
  { name: 'projects', path: '{projectRepo}/projects' },
  { name: 'project board', path: '{project}' },
  { name: 'wiki', path: '{ownRepo}/wiki/{wiki}' },
  { name: 'new wiki page', path: '{ownRepo}/wiki/new' },
  { name: 'edit wiki page', path: '{ownRepo}/wiki/{wiki}/edit' },
  { name: 'releases', path: '{releaseRepo}/releases' },
  { name: 'release', path: '{release}' },
  { name: 'new release', path: '{ownRepo}/releases/new' },

  // Gists, organizations, discovery
  { name: 'gists', path: '/gists' },
  { name: 'gist', path: '{gist}' },
  { name: 'edit gist', path: '{gist}/edit' },
  { name: 'new gist', path: '/gists/new' },
  { name: 'organizations', path: '/organizations' },
  { name: 'new organization', path: '/organizations/new' },
  { name: 'org profile', path: '/{org}' },
  { name: 'org settings', path: '/orgs/{org}/settings' },
  { name: 'topic', path: '/topic/go' },
  { name: 'search', path: '/search?q=a' },
  { name: 'code search', path: '/search/code?q=func' },

  // Instance admin
  { name: 'admin settings', path: '/admin/settings' },
  { name: 'admin users', path: '/admin/users' },
  { name: 'admin user', path: '/admin/users/{user}' },
  { name: 'audit log', path: '/admin/audit-log' },
  { name: 'sso settings', path: '/admin/sso' },

  // Signed out
  { name: 'login', path: '/login', anon: true },
  { name: 'register', path: '/register', anon: true },
  { name: 'forgot password', path: '/auth/password/forgot', anon: true },
  { name: 'reset password', path: '/auth/password/reset/invalid-token', anon: true, status: 400 },
  { name: 'verify email', path: '/verify-email?token=invalid-token', anon: true, status: 400 },
  { name: 'finish sign-up', path: '/register/complete/invalid-token', anon: true },
  { name: 'invite', path: '/invite/invalid-token', anon: true },
];

export function resolve(path, fixtures) {
  return path.replace(/\{(\w+)\}/g, (_, key) => {
    if (!(key in fixtures)) throw new Error(`no fixture for {${key}}`);
    return fixtures[key];
  });
}
