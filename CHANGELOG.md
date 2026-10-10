# Changelog

## [0.7.0](https://github.com/mkappworks-dev/cloudzilla-app/compare/v0.6.0...v0.7.0) (2026-10-10)


### Features

* **admin:** let a superadmin add an organization owner ([#191](https://github.com/mkappworks-dev/cloudzilla-app/issues/191)) ([6f0088a](https://github.com/mkappworks-dev/cloudzilla-app/commit/6f0088aca8f7fb5dbcf83f7872b3dd4dc18ad5aa))
* **api:** per-owner repository count and storage quotas ([#193](https://github.com/mkappworks-dev/cloudzilla-app/issues/193)) ([112bf52](https://github.com/mkappworks-dev/cloudzilla-app/commit/112bf52b9b336b43ba2cf35c83cc015e3d46af2a))
* **auth:** device login approval page for cz auth login ([#214](https://github.com/mkappworks-dev/cloudzilla-app/issues/214)) ([c30abe4](https://github.com/mkappworks-dev/cloudzilla-app/commit/c30abe454b2c5e81f4e18e15a2c28018a842de3b))
* **auth:** device-code login endpoints and grant store ([#207](https://github.com/mkappworks-dev/cloudzilla-app/issues/207)) ([584d4c1](https://github.com/mkappworks-dev/cloudzilla-app/commit/584d4c1226304fe803a3bd0f1dfa04039cf9b562))
* **cli:** cz auth login uses the device flow by default ([#216](https://github.com/mkappworks-dev/cloudzilla-app/issues/216)) ([e3aeda9](https://github.com/mkappworks-dev/cloudzilla-app/commit/e3aeda9dfeee71c0d57aec2bbdb3469533667040))
* **cz:** remote CLI client and cz-admin rename ([#199](https://github.com/mkappworks-dev/cloudzilla-app/issues/199)) ([2848367](https://github.com/mkappworks-dev/cloudzilla-app/commit/28483674e0a3b8b702f1be082dc3045c683299e9))
* **git:** require a pull request on protected branches ([#189](https://github.com/mkappworks-dev/cloudzilla-app/issues/189)) ([93c93df](https://github.com/mkappworks-dev/cloudzilla-app/commit/93c93df84746306474cbd26e1c1bb56f8418a3ec))
* **notifications:** GitHub-style inbox with subject titles and pagination ([#213](https://github.com/mkappworks-dev/cloudzilla-app/issues/213)) ([cb0a4c5](https://github.com/mkappworks-dev/cloudzilla-app/commit/cb0a4c53417198c5372c3953e21ca25277869799))
* **notifications:** per-thread notification subscriptions ([#232](https://github.com/mkappworks-dev/cloudzilla-app/issues/232)) ([c557a5f](https://github.com/mkappworks-dev/cloudzilla-app/commit/c557a5fc1a06bc8320d976ac4c1d27b3661ccb0f))
* **ops:** backup and restore ([#192](https://github.com/mkappworks-dev/cloudzilla-app/issues/192)) ([344bfd5](https://github.com/mkappworks-dev/cloudzilla-app/commit/344bfd51b318796142e26aa571ba53b87e12b469))
* **search:** live suggestions in the topnav search ([#195](https://github.com/mkappworks-dev/cloudzilla-app/issues/195)) ([4f75c9b](https://github.com/mkappworks-dev/cloudzilla-app/commit/4f75c9b58ae24a7bea5d01a4acf9f509782f3ee5))
* **search:** match word prefixes in repo, issue and PR results ([#198](https://github.com/mkappworks-dev/cloudzilla-app/issues/198)) ([03dc3fd](https://github.com/mkappworks-dev/cloudzilla-app/commit/03dc3fddec11ac94f60f746aa63adf46ed6a7c96))
* **search:** redesign the results page ([#200](https://github.com/mkappworks-dev/cloudzilla-app/issues/200)) ([8281142](https://github.com/mkappworks-dev/cloudzilla-app/commit/8281142b91f1b0122bf07c842bae3a94ddc222c5))
* **sso:** verify SAML assertions with goxmldsig ([#239](https://github.com/mkappworks-dev/cloudzilla-app/issues/239)) ([4e77bb5](https://github.com/mkappworks-dev/cloudzilla-app/commit/4e77bb59a673bda5e65d41fb9642c5019d6b4bf7))
* **storage:** markdown image attachments ([#190](https://github.com/mkappworks-dev/cloudzilla-app/issues/190)) ([71c098f](https://github.com/mkappworks-dev/cloudzilla-app/commit/71c098f594dbf2cc86de1a10377cc3b69930add1))
* **ui:** cap the topbar org and repo switchers ([#208](https://github.com/mkappworks-dev/cloudzilla-app/issues/208)) ([fcb9a80](https://github.com/mkappworks-dev/cloudzilla-app/commit/fcb9a808a1a517b5c01c4542d81e9bd8d4335a2a))
* **ui:** clearable, wider topnav search ([#194](https://github.com/mkappworks-dev/cloudzilla-app/issues/194)) ([1e9f246](https://github.com/mkappworks-dev/cloudzilla-app/commit/1e9f246fef29ebd50a1875a6efc90217c9463a29))


### Bug Fixes

* **api:** reject malformed branch-protection patterns with 422 ([#224](https://github.com/mkappworks-dev/cloudzilla-app/issues/224)) ([66a977e](https://github.com/mkappworks-dev/cloudzilla-app/commit/66a977e6d83067ca5ebd438879cc4befed4e0413))
* **auth:** refuse an empty oauth_state cookie in the Google callback ([#222](https://github.com/mkappworks-dev/cloudzilla-app/issues/222)) ([ad0c310](https://github.com/mkappworks-dev/cloudzilla-app/commit/ad0c31008db3bae089a2a5cf2bea28aa9b76f53d))
* close three edge-case gaps found while writing tests ([#202](https://github.com/mkappworks-dev/cloudzilla-app/issues/202)) ([5debbf9](https://github.com/mkappworks-dev/cloudzilla-app/commit/5debbf9e97ec9060a550fc97fc5d335c38fd2448))
* **code:** refuse branch and tag names git check-ref-format rejects ([#225](https://github.com/mkappworks-dev/cloudzilla-app/issues/225)) ([e95cd00](https://github.com/mkappworks-dev/cloudzilla-app/commit/e95cd00825b24650257a1a67a3afc36cac78aa2c))
* **code:** run push side effects for commits made in the browser ([#187](https://github.com/mkappworks-dev/cloudzilla-app/issues/187)) ([e4741e5](https://github.com/mkappworks-dev/cloudzilla-app/commit/e4741e534bffcd40e68ad4b10b65fe7019f69864))
* **db:** renumber the attachments migration to 109 ([#197](https://github.com/mkappworks-dev/cloudzilla-app/issues/197)) ([95ecbaf](https://github.com/mkappworks-dev/cloudzilla-app/commit/95ecbafff8207c1ce9f1fd6f3775b1365e835a05))
* **deps:** move to go1.27.2 and x/net v0.60.0, and scan for vulnerabilities in CI ([#206](https://github.com/mkappworks-dev/cloudzilla-app/issues/206)) ([6050ee2](https://github.com/mkappworks-dev/cloudzilla-app/commit/6050ee26c8db110a9e396ec71db5e9cfd82e836f))
* **projects:** drop cross-repo cards and hide private issue titles on boards ([#223](https://github.com/mkappworks-dev/cloudzilla-app/issues/223)) ([f6459ef](https://github.com/mkappworks-dev/cloudzilla-app/commit/f6459ef3bd3a2081df917b4b581bde5ce1038e6d))
* **projects:** refuse cards that reference another repo's issue or PR ([#205](https://github.com/mkappworks-dev/cloudzilla-app/issues/205)) ([79d996c](https://github.com/mkappworks-dev/cloudzilla-app/commit/79d996c87b07197d68b44203f7e6f73b7ab58da1))
* **pulls:** only writers can set labels and reviewers when opening a PR ([#244](https://github.com/mkappworks-dev/cloudzilla-app/issues/244)) ([184a1cd](https://github.com/mkappworks-dev/cloudzilla-app/commit/184a1cd521adb71181a80892ce2781bf46779cc8))
* **releases:** refuse tag names git rejects with 422 instead of 500 ([#230](https://github.com/mkappworks-dev/cloudzilla-app/issues/230)) ([633fe7a](https://github.com/mkappworks-dev/cloudzilla-app/commit/633fe7ad02be03fe918c03870ce22111b32adbdc))
* return 404, 403 and 422 instead of 500 for missing or invalid input ([#211](https://github.com/mkappworks-dev/cloudzilla-app/issues/211)) ([6990cea](https://github.com/mkappworks-dev/cloudzilla-app/commit/6990cea630d00e034cada2b1ddd26e62d2d185ac))
* **ui:** highlight the active section in org settings nav ([#209](https://github.com/mkappworks-dev/cloudzilla-app/issues/209)) ([b145b0e](https://github.com/mkappworks-dev/cloudzilla-app/commit/b145b0e1cd190fc76123eaa678fca86d1744afb2))
* **ui:** say the PR notifications button watches the repository ([#215](https://github.com/mkappworks-dev/cloudzilla-app/issues/215)) ([4e82994](https://github.com/mkappworks-dev/cloudzilla-app/commit/4e82994080a05f4e22158b1d2947a92d298261d3))


### Refactors

* **service:** split sso_service.go into LDAP, SAML and provisioning files ([#233](https://github.com/mkappworks-dev/cloudzilla-app/issues/233)) ([02b08c8](https://github.com/mkappworks-dev/cloudzilla-app/commit/02b08c8f7acded49552e9707cd4923b3664a02d4))


### Technical

* clarify handler file naming ([#243](https://github.com/mkappworks-dev/cloudzilla-app/issues/243)) ([0422f20](https://github.com/mkappworks-dev/cloudzilla-app/commit/0422f2096b1763654d221a95a5b2358cad434151))
* **handler:** split page_pull_handler.go by concern ([#238](https://github.com/mkappworks-dev/cloudzilla-app/issues/238)) ([a6b0b4a](https://github.com/mkappworks-dev/cloudzilla-app/commit/a6b0b4a026d844f13a40ce70f7d860543d68a53a))
* rename page_*_handler.go to *_page_handler.go and add naming follow-up ticket ([#241](https://github.com/mkappworks-dev/cloudzilla-app/issues/241)) ([cec001e](https://github.com/mkappworks-dev/cloudzilla-app/commit/cec001ea83762a12a6f54b7f3a1f4d153ac46573))
* **service:** split repo_service.go by concern ([#235](https://github.com/mkappworks-dev/cloudzilla-app/issues/235)) ([1fd95a0](https://github.com/mkappworks-dev/cloudzilla-app/commit/1fd95a0c88451a2f092650f590625a984750d8d6))
* split milestone_handler.go into API and page handler files ([#234](https://github.com/mkappworks-dev/cloudzilla-app/issues/234)) ([660a9f3](https://github.com/mkappworks-dev/cloudzilla-app/commit/660a9f33ecdb53f37a42fe13f57a3f3274364bbb))
* **store:** split user_store.go by concern ([#237](https://github.com/mkappworks-dev/cloudzilla-app/issues/237)) ([18195ff](https://github.com/mkappworks-dev/cloudzilla-app/commit/18195ff589fa9f38af5543ec4a84c076226b2b09))


### Documentation

* close Milestone 1 in the ROADMAP, document 36 API routes, add SSO and wiki docs ([#229](https://github.com/mkappworks-dev/cloudzilla-app/issues/229)) ([65736f3](https://github.com/mkappworks-dev/cloudzilla-app/commit/65736f39c53a46221706e727e5668e53543d96f9))
* **cz:** design the device-code login and split ticket 07 ([#201](https://github.com/mkappworks-dev/cloudzilla-app/issues/201)) ([0043450](https://github.com/mkappworks-dev/cloudzilla-app/commit/0043450b95cb12cdcb200cfdc138dd6f0511d2dc))
* document the spec-to-implement planning workflow ([#220](https://github.com/mkappworks-dev/cloudzilla-app/issues/220)) ([9199e9b](https://github.com/mkappworks-dev/cloudzilla-app/commit/9199e9bb1ff11c6bd94356b46ba8bf79595cce68))
* drop the superpowers workflow and docs ([#217](https://github.com/mkappworks-dev/cloudzilla-app/issues/217)) ([4031a94](https://github.com/mkappworks-dev/cloudzilla-app/commit/4031a94c2e9138a47dba2b8291e1f8b61edf3fcc))
* **scratch:** recommend goxmldsig for SAML signature verification ([#236](https://github.com/mkappworks-dev/cloudzilla-app/issues/236)) ([57ad8b0](https://github.com/mkappworks-dev/cloudzilla-app/commit/57ad8b0a0b12fb38673156a403ac85ba9b21e8ff))
* **scratch:** triage the Milestone 1 closing work ([#212](https://github.com/mkappworks-dev/cloudzilla-app/issues/212)) ([805d5ac](https://github.com/mkappworks-dev/cloudzilla-app/commit/805d5aca66081e8f7cd161648086a6ee84735e4f))


### Tests

* cover activity, notifications, event/stats services and cz-admin backup, restore, seed ([#242](https://github.com/mkappworks-dev/cloudzilla-app/issues/242)) ([a93265a](https://github.com/mkappworks-dev/cloudzilla-app/commit/a93265ab0bf07b4f60171eb3084e87b6d851f26a))
* cover backup/restore, the remaining handlers and thin services ([#204](https://github.com/mkappworks-dev/cloudzilla-app/issues/204)) ([9269ac1](https://github.com/mkappworks-dev/cloudzilla-app/commit/9269ac1fa24538e22055b2b377990b59f5dae156))
* cover the lowest-coverage services, stores and handlers ([#188](https://github.com/mkappworks-dev/cloudzilla-app/issues/188)) ([3179a3b](https://github.com/mkappworks-dev/cloudzilla-app/commit/3179a3b9e02026b5a36aa97ff6211fb3c873e892))
* run integration and backup round-trip tests in CI, cover LDAPS binds ([#231](https://github.com/mkappworks-dev/cloudzilla-app/issues/231)) ([393f8f1](https://github.com/mkappworks-dev/cloudzilla-app/commit/393f8f135ec67f374ce7e9cb6d0ca991a2b74308))
* **sso:** cover SSOService and fix two panics on malformed input ([#186](https://github.com/mkappworks-dev/cloudzilla-app/issues/186)) ([76311df](https://github.com/mkappworks-dev/cloudzilla-app/commit/76311df3cf0cb99a273a2e0143bd0c5be34d26d4))

## [0.6.0](https://github.com/mkappworks-dev/cloudzilla-app/compare/v0.5.0...v0.6.0) (2026-10-08)


### Features

* **admin:** issue password reset links from the admin user page ([#183](https://github.com/mkappworks-dev/cloudzilla-app/issues/183)) ([704ec0f](https://github.com/mkappworks-dev/cloudzilla-app/commit/704ec0fc7a3710ba65546ea053488b38b9deea65))
* **admin:** manage user accounts and suspend them everywhere ([#176](https://github.com/mkappworks-dev/cloudzilla-app/issues/176)) ([45c4372](https://github.com/mkappworks-dev/cloudzilla-app/commit/45c4372b2834a8244c8fc4d6c8c0604f676c096d))
* **api:** rate-limit requests per user and per IP ([#169](https://github.com/mkappworks-dev/cloudzilla-app/issues/169)) ([b49b349](https://github.com/mkappworks-dev/cloudzilla-app/commit/b49b34908751af23beb01c8c12c9814d3aa79f87))
* **auth:** password reset by email, with a CLI fallback ([#175](https://github.com/mkappworks-dev/cloudzilla-app/issues/175)) ([79bdcb9](https://github.com/mkappworks-dev/cloudzilla-app/commit/79bdcb9fa86d08c2309e14c66bd601cadb6cee0d))
* **cli:** reset-2fa command for a locked-out operator ([#182](https://github.com/mkappworks-dev/cloudzilla-app/issues/182)) ([b6e9763](https://github.com/mkappworks-dev/cloudzilla-app/commit/b6e9763feee779e3c610fb967094a9d4f39e5db6))
* **code:** edit, rename and delete files from the browser ([#173](https://github.com/mkappworks-dev/cloudzilla-app/issues/173)) ([2c97715](https://github.com/mkappworks-dev/cloudzilla-app/commit/2c97715f61ec063853da5e97f93280b722222c0e))
* **fork:** a create-a-fork page with owner, name and default-branch options ([#149](https://github.com/mkappworks-dev/cloudzilla-app/issues/149)) ([2bd21e2](https://github.com/mkappworks-dev/cloudzilla-app/commit/2bd21e2361a22941c632398d2d98cf4b9c177c33))
* **issues:** close issues from closing keywords ([#174](https://github.com/mkappworks-dev/cloudzilla-app/issues/174)) ([d493529](https://github.com/mkappworks-dev/cloudzilla-app/commit/d493529990a72cc7793273d4107871815bb607fd))
* **mirror:** pull mirrors with scheduled sync, a read-only guard and sealed credentials ([#181](https://github.com/mkappworks-dev/cloudzilla-app/issues/181)) ([e7f9b86](https://github.com/mkappworks-dev/cloudzilla-app/commit/e7f9b8672e55c9ca0ec18e2ea4bd4dbcc75f0965))
* **ops:** add /healthz and /readyz probes and a Docker HEALTHCHECK ([#170](https://github.com/mkappworks-dev/cloudzilla-app/issues/170)) ([dbb0518](https://github.com/mkappworks-dev/cloudzilla-app/commit/dbb0518f324328d400513a780b5162c51ca13692))
* **ops:** Prometheus metrics on a separate listener ([#184](https://github.com/mkappworks-dev/cloudzilla-app/issues/184)) ([b3385d5](https://github.com/mkappworks-dev/cloudzilla-app/commit/b3385d564e892017b3c0db1ffcb048548beb2d75))
* **storage:** avatar uploads on local or S3-compatible storage ([#179](https://github.com/mkappworks-dev/cloudzilla-app/issues/179)) ([db352df](https://github.com/mkappworks-dev/cloudzilla-app/commit/db352df12baa2c3306fdcab8e5c4ed7e89d0014c))
* **ui:** keep the file tree on blob pages ([#146](https://github.com/mkappworks-dev/cloudzilla-app/issues/146)) ([c6f9749](https://github.com/mkappworks-dev/cloudzilla-app/commit/c6f974960c621193fb101992cb9f1fec5518e8ea))
* **ui:** syntax highlighting with per-user code themes ([#150](https://github.com/mkappworks-dev/cloudzilla-app/issues/150)) ([7755d08](https://github.com/mkappworks-dev/cloudzilla-app/commit/7755d087fe7afd27ee475eb83ec128ad5534c90e))


### Bug Fixes

* **a11y:** keep the file tree's aria-expanded in sync with each folder ([#168](https://github.com/mkappworks-dev/cloudzilla-app/issues/168)) ([4d1e48d](https://github.com/mkappworks-dev/cloudzilla-app/commit/4d1e48d7a5f00f71c81963aa4f2ac8689b767b52))
* **code:** answer an unknown archive ref with 404 ([#158](https://github.com/mkappworks-dev/cloudzilla-app/issues/158)) ([ba0cbe1](https://github.com/mkappworks-dev/cloudzilla-app/commit/ba0cbe1fb4240d9c04a2ebfca57f39de57620484))
* **code:** build code-browser links from the full commit SHA ([#152](https://github.com/mkappworks-dev/cloudzilla-app/issues/152)) ([50845af](https://github.com/mkappworks-dev/cloudzilla-app/commit/50845af47103878f98df8457e434d76b948d93f6))
* **code:** escape refs and paths in code-browser URLs ([#167](https://github.com/mkappworks-dev/cloudzilla-app/issues/167)) ([15dfcdb](https://github.com/mkappworks-dev/cloudzilla-app/commit/15dfcdb6654a5a691a7820461f8f9a040b1d8e71))
* **code:** file-tree filter shows nested matches ([#161](https://github.com/mkappworks-dev/cloudzilla-app/issues/161)) ([ecf3954](https://github.com/mkappworks-dev/cloudzilla-app/commit/ecf3954fe1041df67d081e30a3f3e2c8f5df310f))
* **code:** resolve annotated tags in the code browser ([#153](https://github.com/mkappworks-dev/cloudzilla-app/issues/153)) ([42fcbca](https://github.com/mkappworks-dev/cloudzilla-app/commit/42fcbcae3678fb8b77e5342af77ece3e122a62a9))
* **files:** serve raw files so the blob page's Raw button works ([#148](https://github.com/mkappworks-dev/cloudzilla-app/issues/148)) ([bb49cbe](https://github.com/mkappworks-dev/cloudzilla-app/commit/bb49cbe6cb2d3cc1e8b9fc04e7e0725e8c22927f))
* **markdown:** cap fence highlighting per request, not just per document ([#162](https://github.com/mkappworks-dev/cloudzilla-app/issues/162)) ([7cbeead](https://github.com/mkappworks-dev/cloudzilla-app/commit/7cbeeada869bd01d15364ee9e4685c81403038ee))
* **pulls:** check required statuses against the head commit's SHA ([#154](https://github.com/mkappworks-dev/cloudzilla-app/issues/154)) ([261a833](https://github.com/mkappworks-dev/cloudzilla-app/commit/261a8339e6df613e8d911b8f8b580db351472ed1))
* **pulls:** merge the head commit that passed the required checks ([#165](https://github.com/mkappworks-dev/cloudzilla-app/issues/165)) ([ceeccc9](https://github.com/mkappworks-dev/cloudzilla-app/commit/ceeccc938788070d427e639c07020e0f22590e64))
* **repo:** point an empty repo's HEAD at its default branch ([#160](https://github.com/mkappworks-dev/cloudzilla-app/issues/160)) ([b575a7a](https://github.com/mkappworks-dev/cloudzilla-app/commit/b575a7a662056b73450b5422a14f5c470740fd48))
* **security:** answer a private repo over git like a missing repo ([#157](https://github.com/mkappworks-dev/cloudzilla-app/issues/157)) ([315d8ea](https://github.com/mkappworks-dev/cloudzilla-app/commit/315d8ea3f4406fefc88ff5ad8557a5515b42a1a5))
* **security:** answer a private repo's code search like a missing repo's ([#155](https://github.com/mkappworks-dev/cloudzilla-app/issues/155)) ([dd551ab](https://github.com/mkappworks-dev/cloudzilla-app/commit/dd551ab5d8be437732af58f8fc9412773c5baef2))
* **security:** answer a private repo's pages like a missing repo's ([#151](https://github.com/mkappworks-dev/cloudzilla-app/issues/151)) ([8306f09](https://github.com/mkappworks-dev/cloudzilla-app/commit/8306f09b2aa15bec272c240bfcc6e90240b2edcf))
* **security:** include readable private repos in code search ([#164](https://github.com/mkappworks-dev/cloudzilla-app/issues/164)) ([5c1f6a7](https://github.com/mkappworks-dev/cloudzilla-app/commit/5c1f6a7e547e3d6b012a1a411e3c3661df5ae908))
* **settings:** keep the default branch an existing branch and move HEAD with it ([#156](https://github.com/mkappworks-dev/cloudzilla-app/issues/156)) ([6072351](https://github.com/mkappworks-dev/cloudzilla-app/commit/6072351528195272dc8995d182ee8ac33571bdd1))
* **settings:** show account, org and SSO save errors instead of a stale success toast ([#166](https://github.com/mkappworks-dev/cloudzilla-app/issues/166)) ([ac1ac93](https://github.com/mkappworks-dev/cloudzilla-app/commit/ac1ac93b2761b37d9bffd89854cd3169ce7ea34a))
* **settings:** show repo settings save errors instead of a stale success toast ([#163](https://github.com/mkappworks-dev/cloudzilla-app/issues/163)) ([92e8045](https://github.com/mkappworks-dev/cloudzilla-app/commit/92e8045724ad5b0b65aa7e5733ba3051976e62e2))
* **ssh:** refuse a deploy key already registered on another repo ([#178](https://github.com/mkappworks-dev/cloudzilla-app/issues/178)) ([140e777](https://github.com/mkappworks-dev/cloudzilla-app/commit/140e7777167df059e837b75e07f300246b575db2))
* **ssh:** refuse a user SSH key already registered as a deploy key ([#177](https://github.com/mkappworks-dev/cloudzilla-app/issues/177)) ([298963b](https://github.com/mkappworks-dev/cloudzilla-app/commit/298963ba3823ed2e65c45cc8e32a6e28a38d6dd6))
* **ui:** point footer and user-menu help links at the upstream docs ([#143](https://github.com/mkappworks-dev/cloudzilla-app/issues/143)) ([8b27356](https://github.com/mkappworks-dev/cloudzilla-app/commit/8b273562b79d3c806332037538cc8402c36eabe6))
* **ui:** replace the Actions mockup with a Checks tab of commit statuses ([#171](https://github.com/mkappworks-dev/cloudzilla-app/issues/171)) ([1b1b3a7](https://github.com/mkappworks-dev/cloudzilla-app/commit/1b1b3a75227f348a4ca0195a03ea7a53f37b91d1))
* **ui:** show the build version in the footer ([#144](https://github.com/mkappworks-dev/cloudzilla-app/issues/144)) ([daaaf0a](https://github.com/mkappworks-dev/cloudzilla-app/commit/daaaf0acdc43f6334ab6dc3e26dcbec83809c17a))
* **ui:** switch branches from the code browser, including feature/x refs ([#147](https://github.com/mkappworks-dev/cloudzilla-app/issues/147)) ([affd92b](https://github.com/mkappworks-dev/cloudzilla-app/commit/affd92b194cbdac28a4d9f673c8144500e0d3bd1))
* **webhooks:** refuse private addresses at dial time ([#172](https://github.com/mkappworks-dev/cloudzilla-app/issues/172)) ([678541e](https://github.com/mkappworks-dev/cloudzilla-app/commit/678541e5a8a03d31b3a1ac70af1bffc0ccde0704))


### Tests

* **db:** fail when two migrations share a number ([#180](https://github.com/mkappworks-dev/cloudzilla-app/issues/180)) ([6420bc4](https://github.com/mkappworks-dev/cloudzilla-app/commit/6420bc43c24a7c72c989455ca26102b79079907b))

## [0.5.0](https://github.com/mkappworks-dev/cloudzilla-app/compare/v0.4.0...v0.5.0) (2026-10-04)


### ⚠ BREAKING CHANGES

* **deps:** docker-compose deployments on Postgres 17 must dump the database, remove the postgres_data volume and restore into Postgres 18; see docs/deployment.md.

### Features

* **cli:** add seed command for test data ([#126](https://github.com/mkappworks-dev/cloudzilla-app/issues/126)) ([8b29157](https://github.com/mkappworks-dev/cloudzilla-app/commit/8b29157fed6ad3a478ff24cad6098bcb0ca5c828))
* **import:** import a repository from another Git host ([#136](https://github.com/mkappworks-dev/cloudzilla-app/issues/136)) ([7a0c697](https://github.com/mkappworks-dev/cloudzilla-app/commit/7a0c697622bb1916140b2d92bdb4006b0ec85559))
* **ui:** add a show/hide password toggle to sign-in and sign-up forms ([#128](https://github.com/mkappworks-dev/cloudzilla-app/issues/128)) ([6ddeb11](https://github.com/mkappworks-dev/cloudzilla-app/commit/6ddeb11c517c8fb34d273da85eb210b38686f06d))
* **ui:** link the admin pages from the user menu and each other ([#123](https://github.com/mkappworks-dev/cloudzilla-app/issues/123)) ([d8e3a14](https://github.com/mkappworks-dev/cloudzilla-app/commit/d8e3a14afee128d83836d1002c9b833935fcde84))
* **ui:** replace the home shortcuts panel with command palette items ([#125](https://github.com/mkappworks-dev/cloudzilla-app/issues/125)) ([b7dd761](https://github.com/mkappworks-dev/cloudzilla-app/commit/b7dd76197099226a6f183c1adc4eaa8b221fe1ab))


### Bug Fixes

* **api:** refuse to apply left-side suggestions ([#118](https://github.com/mkappworks-dev/cloudzilla-app/issues/118)) ([afdb162](https://github.com/mkappworks-dev/cloudzilla-app/commit/afdb162696d9aa98f4cd0f33c50f1f2dcf812bfe))
* **audit:** drop ORDER BY from the audit log count query ([#115](https://github.com/mkappworks-dev/cloudzilla-app/issues/115)) ([ff231e9](https://github.com/mkappworks-dev/cloudzilla-app/commit/ff231e97b2cb029911235d242f2d7b705aa03311))
* **files:** cap web-commit paths and uploads, refuse .git paths ([#140](https://github.com/mkappworks-dev/cloudzilla-app/issues/140)) ([2e12ec5](https://github.com/mkappworks-dev/cloudzilla-app/commit/2e12ec5c7fa27c4c796ac57612bc834cf038ca1b))
* **files:** keep an upload's name when the New file path ends in / ([#141](https://github.com/mkappworks-dev/cloudzilla-app/issues/141)) ([995ca26](https://github.com/mkappworks-dev/cloudzilla-app/commit/995ca26893bf7b68fbf2b77eb3d88c36f1d775f5))
* **files:** keep existing entries intact when committing from the web ([#139](https://github.com/mkappworks-dev/cloudzilla-app/issues/139)) ([ab5cb1d](https://github.com/mkappworks-dev/cloudzilla-app/commit/ab5cb1da703c10e68d6e51493651b4e9e9875e15))
* **git:** sort tree entries in git's order ([#133](https://github.com/mkappworks-dev/cloudzilla-app/issues/133)) ([fb5ca2b](https://github.com/mkappworks-dev/cloudzilla-app/commit/fb5ca2bfc26a51f9dcba5d3680fe6c3232b0c14d))
* **pulls:** diff PRs from their merge base ([#122](https://github.com/mkappworks-dev/cloudzilla-app/issues/122)) ([bedda55](https://github.com/mkappworks-dev/cloudzilla-app/commit/bedda558cacc3dca96d4509e1940c795c6e247b4))
* **pulls:** keep submodules and mode changes in merged trees ([#130](https://github.com/mkappworks-dev/cloudzilla-app/issues/130)) ([6b56eb0](https://github.com/mkappworks-dev/cloudzilla-app/commit/6b56eb0d7adfd6a6874b3d795b4694ae02a820f2))
* **push:** walk only the commits a push adds ([#129](https://github.com/mkappworks-dev/cloudzilla-app/issues/129)) ([bc2cd0d](https://github.com/mkappworks-dev/cloudzilla-app/commit/bc2cd0d493c0414c5098ea134e14d6e8dd5a6d9e))
* **repo:** seed the initial commit without a git binary ([#137](https://github.com/mkappworks-dev/cloudzilla-app/issues/137)) ([4062623](https://github.com/mkappworks-dev/cloudzilla-app/commit/406262311f7f19224a86b8692c8bdfb9dd97ff2e))
* **security:** cap form bodies before the CSRF check parses them ([#142](https://github.com/mkappworks-dev/cloudzilla-app/issues/142)) ([bcd144d](https://github.com/mkappworks-dev/cloudzilla-app/commit/bcd144da3f0a989631eb80c79ca1dea60b271135))
* **setup:** hide search and footer links on the setup page ([#108](https://github.com/mkappworks-dev/cloudzilla-app/issues/108)) ([8fb35bc](https://github.com/mkappworks-dev/cloudzilla-app/commit/8fb35bc23df5e42b5f85f88759c97d65f4d960d3))
* **ui:** give PR line comment rows selector-safe ids ([#110](https://github.com/mkappworks-dev/cloudzilla-app/issues/110)) ([a682dac](https://github.com/mkappworks-dev/cloudzilla-app/commit/a682dac0ed8a0849bb030032368a45b4c74c6ffc))
* **ui:** number PR context lines by the head file ([#116](https://github.com/mkappworks-dev/cloudzilla-app/issues/116)) ([4bf4f01](https://github.com/mkappworks-dev/cloudzilla-app/commit/4bf4f0119bd7439ec4fe3d462af58fed6c58098d))
* **ui:** show data-toast when the response swaps out its element ([#109](https://github.com/mkappworks-dev/cloudzilla-app/issues/109)) ([a320df7](https://github.com/mkappworks-dev/cloudzilla-app/commit/a320df75e1205611ec450275f90db3a517a166b8))
* **ui:** show left-side PR line comments under their base-file line ([#120](https://github.com/mkappworks-dev/cloudzilla-app/issues/120)) ([8099202](https://github.com/mkappworks-dev/cloudzilla-app/commit/80992021c09b1c9fb1e4400f6e156cedeb3bdfcd))
* **ui:** show toasts after HX-Redirect and HX-Refresh responses ([#111](https://github.com/mkappworks-dev/cloudzilla-app/issues/111)) ([c36e1d8](https://github.com/mkappworks-dev/cloudzilla-app/commit/c36e1d8918d0033938ae9f7a81db7d28586d6d5e))
* **ui:** skip data-toast when a dialog form answers with an error ([#114](https://github.com/mkappworks-dev/cloudzilla-app/issues/114)) ([5286524](https://github.com/mkappworks-dev/cloudzilla-app/commit/52865248c5f68bbebbc09962c28007b12d7a3628))
* **ui:** stop page titles repeating the Cloudzilla suffix ([#124](https://github.com/mkappworks-dev/cloudzilla-app/issues/124)) ([f1065cc](https://github.com/mkappworks-dev/cloudzilla-app/commit/f1065cca3a88000bb238a347804f35f0d1fd2eb5))


### Performance

* **git:** read only the history since branches diverged ([#135](https://github.com/mkappworks-dev/cloudzilla-app/issues/135)) ([4141f69](https://github.com/mkappworks-dev/cloudzilla-app/commit/4141f6977e5ee4bd213c03ce9973ae1146b0c922))
* **pulls:** reuse each PR tab's own loads for the tab badges ([#121](https://github.com/mkappworks-dev/cloudzilla-app/issues/121)) ([9ecd1b1](https://github.com/mkappworks-dev/cloudzilla-app/commit/9ecd1b19a3c2be08c4ad8e7a0a2a9cb265e785e2))
* **pulls:** stop computing an unused diff on the Conversation tab ([#117](https://github.com/mkappworks-dev/cloudzilla-app/issues/117)) ([5b92d49](https://github.com/mkappworks-dev/cloudzilla-app/commit/5b92d49865311863272557efb27ac7b3440d8b46))
* **pulls:** stop PR page views from writing merged trees ([#127](https://github.com/mkappworks-dev/cloudzilla-app/issues/127)) ([d1a98c6](https://github.com/mkappworks-dev/cloudzilla-app/commit/d1a98c6de50d401fd56c913f3d6e7f85a6f73717))
* **push:** check for force pushes only under a rule that blocks them ([#138](https://github.com/mkappworks-dev/cloudzilla-app/issues/138)) ([01301fc](https://github.com/mkappworks-dev/cloudzilla-app/commit/01301fc63614b9f7b19c11b633b474686fb6ccfa))
* **static:** cache and gzip embedded assets, load mermaid on demand ([#107](https://github.com/mkappworks-dev/cloudzilla-app/issues/107)) ([b895fbf](https://github.com/mkappworks-dev/cloudzilla-app/commit/b895fbf6d04b4dd1cac03fdea245d8b2de518dd8))


### Documentation

* **seed:** seed the dev instance into ./git-repos ([#131](https://github.com/mkappworks-dev/cloudzilla-app/issues/131)) ([2ccc803](https://github.com/mkappworks-dev/cloudzilla-app/commit/2ccc803fbe0f02eec2b60ed99f53ddfc0cd00443))
* set up agent skills with a local-markdown issue tracker ([#113](https://github.com/mkappworks-dev/cloudzilla-app/issues/113)) ([d2ce63b](https://github.com/mkappworks-dev/cloudzilla-app/commit/d2ce63b618bbb70b16d413fa3c6335fc5bf47afd))


### Build

* **deps:** move to goldmark v2 ([#105](https://github.com/mkappworks-dev/cloudzilla-app/issues/105)) ([f0da8c7](https://github.com/mkappworks-dev/cloudzilla-app/commit/f0da8c76c9448afb721943eaca403a52b96ed8ea))
* **deps:** upgrade htmx to 2.0.11 ([#106](https://github.com/mkappworks-dev/cloudzilla-app/issues/106)) ([aa85f7c](https://github.com/mkappworks-dev/cloudzilla-app/commit/aa85f7ce71989caf33d3ed4ca4ec4af41cff4724))
* **deps:** upgrade htmx to 4.0.0 ([#119](https://github.com/mkappworks-dev/cloudzilla-app/issues/119)) ([43d477e](https://github.com/mkappworks-dev/cloudzilla-app/commit/43d477e348d8c68e889b75a305e1db4bf8b52045))
* **deps:** upgrade mermaid to 12 ([#104](https://github.com/mkappworks-dev/cloudzilla-app/issues/104)) ([a57d727](https://github.com/mkappworks-dev/cloudzilla-app/commit/a57d72750204ef93d79873eb2c0d3812ab5045cd))
* **deps:** upgrade Tailwind CSS to v4 ([#112](https://github.com/mkappworks-dev/cloudzilla-app/issues/112)) ([7bb274a](https://github.com/mkappworks-dev/cloudzilla-app/commit/7bb274aac7cf592753ef9de32c5e1214d435a01d))
* **deps:** upgrade to Postgres 18, Go 1.27 and current dependencies ([#54](https://github.com/mkappworks-dev/cloudzilla-app/issues/54)) ([ed3ed0c](https://github.com/mkappworks-dev/cloudzilla-app/commit/ed3ed0cf44338742acd66f39931ddf1298a3b043))


### Tests

* keep push auto-maintenance out of every bare-repo fixture ([#134](https://github.com/mkappworks-dev/cloudzilla-app/issues/134)) ([313dca6](https://github.com/mkappworks-dev/cloudzilla-app/commit/313dca614fd57552ff38244043ea0743c493d1d6))
* **service:** keep push auto-maintenance out of the PullDiffStats write check ([#132](https://github.com/mkappworks-dev/cloudzilla-app/issues/132)) ([c1cbd23](https://github.com/mkappworks-dev/cloudzilla-app/commit/c1cbd23ed5f3df33e30431c004c4b585ffecaa83))

## [0.4.0](https://github.com/mkappworks-dev/cloudzilla-app/compare/v0.3.0...v0.4.0) (2026-10-01)


### Features

* **account:** hand a deleted account's content to a ghost user ([#79](https://github.com/mkappworks-dev/cloudzilla-app/issues/79)) ([cb3e0ae](https://github.com/mkappworks-dev/cloudzilla-app/commit/cb3e0aed4373ce03117c7d54a0e790d99b03563d))
* **auth:** connect and disconnect Google from account settings ([#72](https://github.com/mkappworks-dev/cloudzilla-app/issues/72)) ([1383bdd](https://github.com/mkappworks-dev/cloudzilla-app/commit/1383bddf18d67084d7de14723455bbd92b1abfaa))
* **auth:** verify email addresses and link Google sign-in to verified ones ([#73](https://github.com/mkappworks-dev/cloudzilla-app/issues/73)) ([202800d](https://github.com/mkappworks-dev/cloudzilla-app/commit/202800dfc7c22e49acd79a6ba9d8f0773c422c08))
* **org:** make organizations own their repositories ([#80](https://github.com/mkappworks-dev/cloudzilla-app/issues/80)) ([cd87f2e](https://github.com/mkappworks-dev/cloudzilla-app/commit/cd87f2e7b0bca255679be5de19cfc69fb550d096))
* **repos:** require the recipient to accept a repository transfer ([#97](https://github.com/mkappworks-dev/cloudzilla-app/issues/97)) ([2003218](https://github.com/mkappworks-dev/cloudzilla-app/commit/2003218618ea190aeba023bbd1ac00d4c7fd8264))
* **settings:** keep commit author email private by default ([#56](https://github.com/mkappworks-dev/cloudzilla-app/issues/56)) ([2c97530](https://github.com/mkappworks-dev/cloudzilla-app/commit/2c97530da056e17e57d01979a74e90bc23cb2075))
* **ui:** UI overhaul phase 7 — gists ([#43](https://github.com/mkappworks-dev/cloudzilla-app/issues/43)) ([90504f3](https://github.com/mkappworks-dev/cloudzilla-app/commit/90504f3f6ef5acbdf8ad216fdec6b2de85d37465))
* **ui:** UI overhaul phase 8 — dashboard ([#44](https://github.com/mkappworks-dev/cloudzilla-app/issues/44)) ([bdcee6b](https://github.com/mkappworks-dev/cloudzilla-app/commit/bdcee6b8067d45fb34f2ddfe26ef9bcbff1fcd6c))
* **ui:** UI overhaul phase 9 — profile and organizations ([#45](https://github.com/mkappworks-dev/cloudzilla-app/issues/45)) ([0b8cc69](https://github.com/mkappworks-dev/cloudzilla-app/commit/0b8cc69e3808ea8f89672197cd873963987b840d))


### Bug Fixes

* accept thin packs in git receive-pack (HTTP + SSH) ([#47](https://github.com/mkappworks-dev/cloudzilla-app/issues/47)) ([52a6f21](https://github.com/mkappworks-dev/cloudzilla-app/commit/52a6f2137a858b82ef1861491da37ac0ff0dc570))
* **api:** bind line-comment IDs to the pull request in the URL ([#63](https://github.com/mkappworks-dev/cloudzilla-app/issues/63)) ([33a1ae1](https://github.com/mkappworks-dev/cloudzilla-app/commit/33a1ae1935ae0b0f0dec3a082fa2efcd40725a6c))
* **api:** give non-readers the missing-repo 404 on repo API routes ([#59](https://github.com/mkappworks-dev/cloudzilla-app/issues/59)) ([d5ead16](https://github.com/mkappworks-dev/cloudzilla-app/commit/d5ead1633c68ce49b565ca4ed8eb8132500f9a1c))
* **api:** stop serving users' emails and email settings to other people ([#53](https://github.com/mkappworks-dev/cloudzilla-app/issues/53)) ([34b4384](https://github.com/mkappworks-dev/cloudzilla-app/commit/34b43849e44ed0a82145e89f543aa1dd5289d6ed))
* **auth:** close email disclosures and add email-verified signup ([#60](https://github.com/mkappworks-dev/cloudzilla-app/issues/60)) ([56c7ec8](https://github.com/mkappworks-dev/cloudzilla-app/commit/56c7ec8ef049d292656cce87477eccb3b51af6a2))
* **auth:** drop the 'owner' role from repository permissions ([#102](https://github.com/mkappworks-dev/cloudzilla-app/issues/102)) ([f2bdac8](https://github.com/mkappworks-dev/cloudzilla-app/commit/f2bdac8d2891278ee17a6ad3ab4e37b5b5c65a12))
* **auth:** enforce OAuth-app token scopes ([#58](https://github.com/mkappworks-dev/cloudzilla-app/issues/58)) ([9fcb6e3](https://github.com/mkappworks-dev/cloudzilla-app/commit/9fcb6e3d903ee782113dca93bda4fa6c201ee553))
* **auth:** enforce personal access token scopes ([#87](https://github.com/mkappworks-dev/cloudzilla-app/issues/87)) ([7d19146](https://github.com/mkappworks-dev/cloudzilla-app/commit/7d19146522d4341a701cfbc7c61c4a58589b8e3e))
* **auth:** harden the OAuth authorization-code flow ([#61](https://github.com/mkappworks-dev/cloudzilla-app/issues/61)) ([0310ae0](https://github.com/mkappworks-dev/cloudzilla-app/commit/0310ae0a25b7ba5bac40330c861f54c24a3a38d3))
* **auth:** hide owner-only admin controls from admin collaborators ([#95](https://github.com/mkappworks-dev/cloudzilla-app/issues/95)) ([9bbaa6a](https://github.com/mkappworks-dev/cloudzilla-app/commit/9bbaa6ad050166129fc940568f6e58f185718a64))
* **auth:** let the account menu's Sign out submit and redirect home ([#51](https://github.com/mkappworks-dev/cloudzilla-app/issues/51)) ([45f1f7f](https://github.com/mkappworks-dev/cloudzilla-app/commit/45f1f7f5dfa27c2983522bfbb8cc6cea93a38066))
* **auth:** list only repos the viewer can read ([#90](https://github.com/mkappworks-dev/cloudzilla-app/issues/90)) ([23ffd98](https://github.com/mkappworks-dev/cloudzilla-app/commit/23ffd982eca06b68000ac789e432a6a58e7ed8b4))
* **auth:** make TOTP backup codes verifiable ([#74](https://github.com/mkappworks-dev/cloudzilla-app/issues/74)) ([61d13fb](https://github.com/mkappworks-dev/cloudzilla-app/commit/61d13fb728c9039a51b53fc7655f85d8ed17efb1))
* **auth:** mark /oauth/token responses Cache-Control: no-store ([#65](https://github.com/mkappworks-dev/cloudzilla-app/issues/65)) ([9f59073](https://github.com/mkappworks-dev/cloudzilla-app/commit/9f59073e46b28717803d161df8f80d11c22ea2a5))
* **auth:** only repo owners grant, change or remove the admin role ([#93](https://github.com/mkappworks-dev/cloudzilla-app/issues/93)) ([f388e23](https://github.com/mkappworks-dev/cloudzilla-app/commit/f388e231ebdafa1d9036bd1b6a5c2e1ef8acef5a))
* **auth:** require TOTP on API login ([#70](https://github.com/mkappworks-dev/cloudzilla-app/issues/70)) ([325a2c4](https://github.com/mkappworks-dev/cloudzilla-app/commit/325a2c40c317dcfd88da6d4c90189dee5e7aac64))
* **auth:** require TOTP on LDAP, SAML and Google sign-in ([#69](https://github.com/mkappworks-dev/cloudzilla-app/issues/69)) ([d68c4b1](https://github.com/mkappworks-dev/cloudzilla-app/commit/d68c4b120bcfa53195078af129f614f0931718a0))
* **auth:** return RFC 6749 errors from /oauth/token and accept Basic client auth ([#64](https://github.com/mkappworks-dev/cloudzilla-app/issues/64)) ([8d144df](https://github.com/mkappworks-dev/cloudzilla-app/commit/8d144df3293b07b86489955c15f211604c4d5926))
* **auth:** return to the requested page after sign-in ([#62](https://github.com/mkappworks-dev/cloudzilla-app/issues/62)) ([660bcd6](https://github.com/mkappworks-dev/cloudzilla-app/commit/660bcd64abbc46b9e55344c7c6e1771dcdbf219f))
* **git:** accept delete-only pushes without a pack ([#82](https://github.com/mkappworks-dev/cloudzilla-app/issues/82)) ([3ffa670](https://github.com/mkappworks-dev/cloudzilla-app/commit/3ffa6703fd2eeb232b0edde841dbace5e9a6ae34))
* **git:** check pushed history is connected, not just its tip ([#78](https://github.com/mkappworks-dev/cloudzilla-app/issues/78)) ([b2815b8](https://github.com/mkappworks-dev/cloudzilla-app/commit/b2815b8055101c4f52e3fcbe909bf89eb95ffa2a))
* **git:** enforce branch protection before receive-pack writes the ref ([#68](https://github.com/mkappworks-dev/cloudzilla-app/issues/68)) ([9ccd79e](https://github.com/mkappworks-dev/cloudzilla-app/commit/9ccd79e38e88609bfa68c20f1f845b3a7efdd064))
* **git:** keep server paths out of receive-pack unpack errors ([#85](https://github.com/mkappworks-dev/cloudzilla-app/issues/85)) ([7dcab34](https://github.com/mkappworks-dev/cloudzilla-app/commit/7dcab34a17a81a5f25dd913fb09c8f4118ec163e))
* **git:** refuse protected-branch pushes that can't be proven fast-forward ([#71](https://github.com/mkappworks-dev/cloudzilla-app/issues/71)) ([292b7ff](https://github.com/mkappworks-dev/cloudzilla-app/commit/292b7ff7654bb9ab02c682b50b6f562be86755d9))
* **git:** refuse pushes to missing objects and keep server errors out of push status ([#77](https://github.com/mkappworks-dev/cloudzilla-app/issues/77)) ([cc4b466](https://github.com/mkappworks-dev/cloudzilla-app/commit/cc4b4668447c5c0a1d5a69028f87abe78bfa2b41))
* **git:** refuse pushes whose ref moved after the client read it ([#67](https://github.com/mkappworks-dev/cloudzilla-app/issues/67)) ([1d2912f](https://github.com/mkappworks-dev/cloudzilla-app/commit/1d2912f174e21a1c667379b2e76d704a4a37f0f4))
* **git:** refuse web commits when a push moved the branch mid-write ([#66](https://github.com/mkappworks-dev/cloudzilla-app/issues/66)) ([6ff2f26](https://github.com/mkappworks-dev/cloudzilla-app/commit/6ff2f266e81ef995becb37cd60b72d4b9cd07262))
* **git:** refuse web deletes of branches that block force pushes ([#76](https://github.com/mkappworks-dev/cloudzilla-app/issues/76)) ([9c5c0a4](https://github.com/mkappworks-dev/cloudzilla-app/commit/9c5c0a4cc0bcae2e917561d611dcb257f5224d0f))
* hide database errors on forms and require read access on new issue/PR pages ([#57](https://github.com/mkappworks-dev/cloudzilla-app/issues/57)) ([6c013e9](https://github.com/mkappworks-dev/cloudzilla-app/commit/6c013e947230288d9c4cdf48abf6b8e1efd7c9c7))
* **issues:** hide private issues from pinned and milestone lists ([#98](https://github.com/mkappworks-dev/cloudzilla-app/issues/98)) ([e4731c7](https://github.com/mkappworks-dev/cloudzilla-app/commit/e4731c7f75c30521c3d275aacb71f265e83e6b35))
* **issues:** hide private issues from PR linked issues and the Issues tab count ([#101](https://github.com/mkappworks-dev/cloudzilla-app/issues/101)) ([a215b07](https://github.com/mkappworks-dev/cloudzilla-app/commit/a215b07fe22ad67194f8d2a06e7312d1c876d836))
* **issues:** show private issues to repo and org owners ([#94](https://github.com/mkappworks-dev/cloudzilla-app/issues/94)) ([e75fb46](https://github.com/mkappworks-dev/cloudzilla-app/commit/e75fb467ea04f78ef44dfe591a98c292dacc801f))
* **notifications:** only notify users who can still read the repo ([#92](https://github.com/mkappworks-dev/cloudzilla-app/issues/92)) ([39ed6f4](https://github.com/mkappworks-dev/cloudzilla-app/commit/39ed6f40c1e5b39acfefb6d3d0170cf24f562b1e))
* **search:** hide private issues and soft-deleted repos from search ([#91](https://github.com/mkappworks-dev/cloudzilla-app/issues/91)) ([bfdebf1](https://github.com/mkappworks-dev/cloudzilla-app/commit/bfdebf1b52a78193d8a655aa9b28f6862512670c))
* **ssh:** send session errors to stderr so git shows them ([#88](https://github.com/mkappworks-dev/cloudzilla-app/issues/88)) ([7707a8f](https://github.com/mkappworks-dev/cloudzilla-app/commit/7707a8f753db25d6b57207e8d40699ffce63518e))
* **stats:** dedupe commit stats per SHA and add stats backfill command ([#48](https://github.com/mkappworks-dev/cloudzilla-app/issues/48)) ([f890283](https://github.com/mkappworks-dev/cloudzilla-app/commit/f890283495ef1e6b6f3171d5dde8628de4412122))
* **store:** remove broken, unused ListReposByTopic ([#75](https://github.com/mkappworks-dev/cloudzilla-app/issues/75)) ([95c2e28](https://github.com/mkappworks-dev/cloudzilla-app/commit/95c2e2862990de91abb7de4587778490e515b469))
* **ui:** callers' classes reach component elements ([#50](https://github.com/mkappworks-dev/cloudzilla-app/issues/50)) ([efa2bfc](https://github.com/mkappworks-dev/cloudzilla-app/commit/efa2bfc0049acaaf83a99a4e2c490dfe0d483323))
* **ui:** let callers override type on CommandItem, Toggle and DropdownMenuTrigger ([#52](https://github.com/mkappworks-dev/cloudzilla-app/issues/52)) ([16297fe](https://github.com/mkappworks-dev/cloudzilla-app/commit/16297fec07f3b9a9800e85bf1da3fef7d7ac94b1))


### Technical

* enforce gofmt via golangci-lint ([#86](https://github.com/mkappworks-dev/cloudzilla-app/issues/86)) ([7076b0f](https://github.com/mkappworks-dev/cloudzilla-app/commit/7076b0f9ade90fe098497d6658be094a7d8b96af))
* lint backlog ([#55](https://github.com/mkappworks-dev/cloudzilla-app/issues/55)) ([5b937d9](https://github.com/mkappworks-dev/cloudzilla-app/commit/5b937d909032dc3fc743142fd7a8b27492ab0317))


### Tests

* clean up organizations that integration tests leak ([#83](https://github.com/mkappworks-dev/cloudzilla-app/issues/83)) ([3760784](https://github.com/mkappworks-dev/cloudzilla-app/commit/37607849a468e767d876dcdb84ef8ce383c689a6))
* clean up users and repositories that integration tests leak ([#81](https://github.com/mkappworks-dev/cloudzilla-app/issues/81)) ([0efdb3e](https://github.com/mkappworks-dev/cloudzilla-app/commit/0efdb3e90eccdb4d2e8b0006d175fad72d37bd31))
* **router:** keep an account alive in newVerificationRouter ([#99](https://github.com/mkappworks-dev/cloudzilla-app/issues/99)) ([21b0704](https://github.com/mkappworks-dev/cloudzilla-app/commit/21b0704f3720563979a0735bd7e569cae96b3dd0))
* **ssh:** add a receive-pack harness that drives pushes over SSH ([#89](https://github.com/mkappworks-dev/cloudzilla-app/issues/89)) ([71f203c](https://github.com/mkappworks-dev/cloudzilla-app/commit/71f203cf93bf10e7e19324b3f09d2b64280cc039))
* stop seeding an 'owner' permissions row for repo owners ([#96](https://github.com/mkappworks-dev/cloudzilla-app/issues/96)) ([0075e3c](https://github.com/mkappworks-dev/cloudzilla-app/commit/0075e3cff3f5453afb3838b605273eb785495261))
* stop the repo handler tests writing repos into the source tree ([#84](https://github.com/mkappworks-dev/cloudzilla-app/issues/84)) ([9050db4](https://github.com/mkappworks-dev/cloudzilla-app/commit/9050db499ef02d700b7161f286a61dcb4d682e1e))
* use UniqueSuffix for test data names so -count&gt;1 passes ([#49](https://github.com/mkappworks-dev/cloudzilla-app/issues/49)) ([180db1d](https://github.com/mkappworks-dev/cloudzilla-app/commit/180db1d9e672b08a8ea9fd1728ff2358e352e731))

## [0.3.0](https://github.com/mkappworks-dev/cloudzilla-app/compare/v0.2.0...v0.3.0) (2026-05-20)


### Features

* add publish step to promote draft releases to published ([420b44b](https://github.com/mkappworks-dev/cloudzilla-app/commit/420b44b943dcb62a486acae819347806735ecae8))
* phase 6 UI overhaul — wiki, releases, discussions, settings polish ([#42](https://github.com/mkappworks-dev/cloudzilla-app/issues/42)) ([0e3a228](https://github.com/mkappworks-dev/cloudzilla-app/commit/0e3a2286a2c680eaa322adf2a8b82b4e9d950176))
* **ui:** implement phase 9 of UI overhaul for profile and organizations ([778661b](https://github.com/mkappworks-dev/cloudzilla-app/commit/778661b0ccc185ee5e61d60df207a2f1a8ec143e))
* **ui:** repo Insights overhaul — commits, contributors, pulse, dependencies ([#39](https://github.com/mkappworks-dev/cloudzilla-app/issues/39)) ([d6b46cc](https://github.com/mkappworks-dev/cloudzilla-app/commit/d6b46cc7bad8670bb5dd95cd05bb716a5f1709db))
* **ui:** UI overhaul phase 0 — foundation ([#35](https://github.com/mkappworks-dev/cloudzilla-app/issues/35)) ([0545892](https://github.com/mkappworks-dev/cloudzilla-app/commit/0545892486c86d2bb44b4916e0bf0b08220cb060))
* **ui:** UI overhaul phase 1 — flagship pages ([#36](https://github.com/mkappworks-dev/cloudzilla-app/issues/36)) ([369158b](https://github.com/mkappworks-dev/cloudzilla-app/commit/369158b028616836ecb4e67567e200733fac441a))
* **ui:** UI overhaul phase 2 — code browser + project boards ([#37](https://github.com/mkappworks-dev/cloudzilla-app/issues/37)) ([6be4000](https://github.com/mkappworks-dev/cloudzilla-app/commit/6be400012e2703f177d96f38420eaeb3c0eda45d))
* **ui:** UI overhaul phase 4 — tracker pages, Actions, review requests ([#40](https://github.com/mkappworks-dev/cloudzilla-app/issues/40)) ([87276ff](https://github.com/mkappworks-dev/cloudzilla-app/commit/87276ffc75be7dca9b41734f62d64e03c3bad50e))
* **ui:** UI overhaul phase 5 — PR sub-views, account navigation, and milestone redesign ([#41](https://github.com/mkappworks-dev/cloudzilla-app/issues/41)) ([30643f0](https://github.com/mkappworks-dev/cloudzilla-app/commit/30643f087827bdb083b5da9cd582a36c5bdf5757))


### Technical

* **ui:** design system foundation — port all pages + expand component library ([#34](https://github.com/mkappworks-dev/cloudzilla-app/issues/34)) ([36c8d6b](https://github.com/mkappworks-dev/cloudzilla-app/commit/36c8d6b1997364a5433e4a8d69d65e157787d52a))


### Documentation

* add UI overhaul design spec ([7ed3773](https://github.com/mkappworks-dev/cloudzilla-app/commit/7ed3773e4331967cdf3895fca5c18878e0e4df26))
* update CLAUDE.md for clarity and consistency in project details ([395763f](https://github.com/mkappworks-dev/cloudzilla-app/commit/395763f600cc19a2aa6e05f2ed81d57b1f0f7c03))


### CI

* harden GitHub Actions workflows ([#38](https://github.com/mkappworks-dev/cloudzilla-app/issues/38)) ([78d55ed](https://github.com/mkappworks-dev/cloudzilla-app/commit/78d55ed6580a1f80d511e248871da67eca102d29))

## [0.2.0](https://github.com/mkappworks-dev/cloudzilla-app/compare/v0.1.0...v0.2.0) (2026-05-12)


### Features

* **access-token:** implement Personal Access Tokens (Phase 5.1) ([5bdd229](https://github.com/mkappworks-dev/cloudzilla-app/commit/5bdd22959cf3bde5945a4c24e8286eefd5526fe0))
* add admin CLI with cobra ([2f5b510](https://github.com/mkappworks-dev/cloudzilla-app/commit/2f5b510426bc3d7879ba242d135a74021ab88807))
* add auth-aware navigation with BasePage viewmodel ([9f67ba5](https://github.com/mkappworks-dev/cloudzilla-app/commit/9f67ba548d69f2e5733198fe82ceeb3835ccc4e1))
* add business logic layer with service methods ([64d5929](https://github.com/mkappworks-dev/cloudzilla-app/commit/64d59291fd32dd02216e38a7ab2c5915d71faa33))
* add clone URLs to repository page ([14d0852](https://github.com/mkappworks-dev/cloudzilla-app/commit/14d0852b483bbe18001feab198611f5dbc32f526))
* add commit history and diff rendering ([572b6dc](https://github.com/mkappworks-dev/cloudzilla-app/commit/572b6dc7d65da67e473243c2f071b9379f15b5d1))
* add configuration system with viper + YAML support ([f881179](https://github.com/mkappworks-dev/cloudzilla-app/commit/f88117979f37cce12f48dd8ecf45af87b4d31d6e))
* add data access layer with SQL queries ([b99af12](https://github.com/mkappworks-dev/cloudzilla-app/commit/b99af123a43e50517884366a926472f566c15ec8))
* add database layer with migrations and domain models ([0adc84b](https://github.com/mkappworks-dev/cloudzilla-app/commit/0adc84b41c9f3c2fc79c4a3bde37b479ff8159c9))
* add Git HTTP smart protocol handler ([0b8efae](https://github.com/mkappworks-dev/cloudzilla-app/commit/0b8efae0eb1206f65127fb8946294695f76dcd26))
* add Google OAuth authentication ([432688e](https://github.com/mkappworks-dev/cloudzilla-app/commit/432688ee08396ae4c0e7b205b4983d16f6acc185))
* add HTML templates and frontend assets ([afa4f91](https://github.com/mkappworks-dev/cloudzilla-app/commit/afa4f9155e4ef7c07561e1ccd2d9f94b80a93d73))
* add HTTP handlers for auth, users, repos, issues, and pull requests ([d8c12dd](https://github.com/mkappworks-dev/cloudzilla-app/commit/d8c12dd515c2c9287829b95f13d1c4714cfbb5d4))
* add HTTP server entrypoint with embedded templates and static files ([27fabf7](https://github.com/mkappworks-dev/cloudzilla-app/commit/27fabf7a94e546c6bfe92e45ca5e2cbb8465e4da))
* add middleware for auth, logging, and CORS ([0deacee](https://github.com/mkappworks-dev/cloudzilla-app/commit/0deacee679566791a365b6db27fe701e8f1f1cbd))
* add public user registration gated on allow_registration ([f410e3f](https://github.com/mkappworks-dev/cloudzilla-app/commit/f410e3f956312c06d1022393955079e4734218a8))
* add SSH key data model and storage layer ([e9e8cf7](https://github.com/mkappworks-dev/cloudzilla-app/commit/e9e8cf79dde10cd1d7d5559dc778d97398478778))
* add SSH key HTTP API endpoints ([f38d09d](https://github.com/mkappworks-dev/cloudzilla-app/commit/f38d09d3235406408c4a9964adf29b692b0079c1))
* add SSH key service business logic ([080ae90](https://github.com/mkappworks-dev/cloudzilla-app/commit/080ae90b3652c807e785beff01c38b8f5101a3e8))
* add SSH server for git operations ([d44fcc5](https://github.com/mkappworks-dev/cloudzilla-app/commit/d44fcc5048777ecf89ea9dd4d459e5ba83420f28))
* **admin:** add audit log with superadmin viewer (Phase 8.2) ([a1c4508](https://github.com/mkappworks-dev/cloudzilla-app/commit/a1c4508d65a52001be178e4f4a9d3287b18f085b))
* **assignees:** add assignee store, service, and handler ([4b96faa](https://github.com/mkappworks-dev/cloudzilla-app/commit/4b96faac9af0e68eef7b2b324121162432b6cd80))
* **auth:** add LDAP and SAML SSO authentication (Phase 8.3) ([be76fbb](https://github.com/mkappworks-dev/cloudzilla-app/commit/be76fbb32d7d3fe35c21a14dd8c926946e9fedfa))
* **auth:** add TOTP two-factor authentication (Phase 8.1) ([a8914d6](https://github.com/mkappworks-dev/cloudzilla-app/commit/a8914d6c6e9cadf7a1eabb35fe26adde024a20ac))
* **branch-protection:** implement Phase 6.1 Protected Branches ([e799664](https://github.com/mkappworks-dev/cloudzilla-app/commit/e79966464448664d58724d9c4e07803ad4c24122))
* branded 404/403 pages and login redirect for unauthenticated HTML ([c8281cf](https://github.com/mkappworks-dev/cloudzilla-app/commit/c8281cfe8ce6d58d916dac36004ad71d1c172c2f))
* **code:** add GetIssueTemplates and GetPRTemplate to CodeService ([4265ed3](https://github.com/mkappworks-dev/cloudzilla-app/commit/4265ed32dca2a4f453a3d8d0937e5e1f9dffb9cb))
* **codeowners:** implement Phase 6.2 CODEOWNERS auto-assign ([c912007](https://github.com/mkappworks-dev/cloudzilla-app/commit/c91200797d2d60a1b5428550fca1a2771d0dea98))
* **collaborators:** add repo collaborator management and ownership transfer ([6b12163](https://github.com/mkappworks-dev/cloudzilla-app/commit/6b12163bdab6e37b43f3fa72e1f1b8b270dc22de))
* **db:** rewrite all migrations for PostgreSQL ([ac13b97](https://github.com/mkappworks-dev/cloudzilla-app/commit/ac13b97d7da3a3908ea93ed2fc6a9bb95b0e78fa))
* **deploy-key:** implement Deploy Keys (Phase 5.2) ([0c1e69b](https://github.com/mkappworks-dev/cloudzilla-app/commit/0c1e69b0ecec5828eee5514733eb4a5a6bf1e133))
* **deps:** add goldmark markdown rendering library ([597445b](https://github.com/mkappworks-dev/cloudzilla-app/commit/597445be8f292b0c819bdce631d8eb537880b40f))
* **docker:** containerize Cloudzilla with multi-stage build ([b89a006](https://github.com/mkappworks-dev/cloudzilla-app/commit/b89a0061fcb7e4860c6165f2abc386232cf6f0d2))
* extract shared Templ components and apply across pages/fragments ([dcbd614](https://github.com/mkappworks-dev/cloudzilla-app/commit/dcbd6145eebd53f287a34f621e799d8a6bcaabb3))
* **handler,router,templates:** wire fork feature into HTTP layer ([f64a18b](https://github.com/mkappworks-dev/cloudzilla-app/commit/f64a18bfffdcd9184fd58e88eadaa7591cad8e16))
* **handler,router,templates:** wire PR reviews, line comments, and search into HTTP layer ([4934454](https://github.com/mkappworks-dev/cloudzilla-app/commit/4934454f2d27ce41063cff4bcbac0ae7db85ec4c))
* **handler,router,templates:** wire releases, commit status API, and milestones ([79532ec](https://github.com/mkappworks-dev/cloudzilla-app/commit/79532ec5939439ef977f38e8b547dc74a35a227b))
* **handler:** add auto-merge routing to UpdatePull and tryAutoMerge helper ([7a57588](https://github.com/mkappworks-dev/cloudzilla-app/commit/7a575880b199700050ce04c59968c94908acb87f))
* **handler:** add refs page and branch/tag API handlers ([bdf313e](https://github.com/mkappworks-dev/cloudzilla-app/commit/bdf313e244ef90f1ffba34826d6f61bd4c83a3f9))
* **handler:** add template support and page rendering ([0723eb2](https://github.com/mkappworks-dev/cloudzilla-app/commit/0723eb205e3f18a3b8f889ac564f02a4eaa7cb9c))
* **handler:** pass auto-merge state to PullDetailData in PagePullDetail ([0b0b65e](https://github.com/mkappworks-dev/cloudzilla-app/commit/0b0b65e8837376670ec0fda34c65ed4803a1a595))
* **handler:** trigger tryAutoMerge after review submission and commit status update ([066c83b](https://github.com/mkappworks-dev/cloudzilla-app/commit/066c83b7f5070a931c4104cbc868872e097bbc16))
* **invitations:** add superadmin invitation system ([e75c443](https://github.com/mkappworks-dev/cloudzilla-app/commit/e75c443349b06a04c1850d8c2b631aeeca3386ba))
* **issues:** add issue pinning and locking (phase 9.3) ([f7e2796](https://github.com/mkappworks-dev/cloudzilla-app/commit/f7e27960e8418cbd4768384457c1f0b197d47ed1))
* **labels,assignees,stars:** extend page handlers, viewmodels, routes, and templates ([4dac356](https://github.com/mkappworks-dev/cloudzilla-app/commit/4dac356b1a5b6d7c007ffcb0f6b9e7e04be1c6fc))
* **labels:** add label model, store, service, and handler ([12e1661](https://github.com/mkappworks-dev/cloudzilla-app/commit/12e166129bf6d67b6c6236c599ef67fabaae9a5b))
* **markdown:** render markdown in issues, PRs, comments, and repo README ([f6b3460](https://github.com/mkappworks-dev/cloudzilla-app/commit/f6b3460f7dffe5d960053efc781f4654a123323b))
* **mermaid:** self-host mermaid.min.js for diagram rendering ([19d3c98](https://github.com/mkappworks-dev/cloudzilla-app/commit/19d3c98107e14e97947ccae2d904e7350f07ac55))
* **migration:** add 032_add_auto_merge_to_pulls.sql ([a17cc3a](https://github.com/mkappworks-dev/cloudzilla-app/commit/a17cc3a16baec20df25fdb28de7b6c0299dd0a6c))
* **migration:** add fork columns to repositories table (019) ([463d70d](https://github.com/mkappworks-dev/cloudzilla-app/commit/463d70d8e704b1da361c83e9a07728013280d1e7))
* **migration:** add PR reviews, line comments, and search migrations (023-025) ([4836af8](https://github.com/mkappworks-dev/cloudzilla-app/commit/4836af8f996b652615e5e975cef7abdd94e1e102))
* **migration:** add releases, commit statuses, and milestones migrations (020-022) ([ec7fde3](https://github.com/mkappworks-dev/cloudzilla-app/commit/ec7fde3741c67142fa63230c5fc4e07a33ae302f))
* **migrations:** add labels, assignees, and stars migrations (016-018) ([9891136](https://github.com/mkappworks-dev/cloudzilla-app/commit/9891136339b4b33ce8fdeb6fd5b37e4ca189e33c))
* **model,store,service:** implement PR line comment logic ([1cb6da2](https://github.com/mkappworks-dev/cloudzilla-app/commit/1cb6da2243788aca1adf26997cbb853821c17683))
* **model,store,service:** implement PR review logic ([b8e1eb6](https://github.com/mkappworks-dev/cloudzilla-app/commit/b8e1eb6cbef1313461183a82aad5405d15e41bea))
* **model,store:** add fork fields and store methods for repository fork ([9d70e26](https://github.com/mkappworks-dev/cloudzilla-app/commit/9d70e26c46e208c054131cbf2b56459fe9272954))
* **model:** add AutoMergeEnabled/Strategy fields to PullRequest ([1057ea2](https://github.com/mkappworks-dev/cloudzilla-app/commit/1057ea2dad0237042201ad3a00592c1433e50d17))
* **model:** add Release, CommitStatus, Milestone models; extend Issue and Pull ([006f17a](https://github.com/mkappworks-dev/cloudzilla-app/commit/006f17a01c663145c8d774bf1dbb4928753ba99d))
* **notifications:** add in-app notifications ([f34b035](https://github.com/mkappworks-dev/cloudzilla-app/commit/f34b03527b63a2ac66a796ffc0914ef0a3cf31e1))
* **org:** add organization accounts ([a23a890](https://github.com/mkappworks-dev/cloudzilla-app/commit/a23a8908a21ed559f7804fe1b57d5b6f610cb188))
* **orgs:** add org ownership transfer and tighten repo creation ([a046e1d](https://github.com/mkappworks-dev/cloudzilla-app/commit/a046e1d3e45dcc1116cbee752467b563f9af17ed))
* Overhaul documentation by extracting API reference and configuration details ([0b8bbc4](https://github.com/mkappworks-dev/cloudzilla-app/commit/0b8bbc4300d2d6bc15eb8fd8934fd56a7e28da5f))
* **phase-10.1:** add Pulse and Contributors insight pages ([0a53833](https://github.com/mkappworks-dev/cloudzilla-app/commit/0a538334cea64eb4f8b8c1e2db2b6134d932bdf2))
* **phase-10.2:** add [@mention](https://github.com/mention) parsing, notifications, and link rendering ([3e83602](https://github.com/mkappworks-dev/cloudzilla-app/commit/3e83602c38510bccc0cba5fcfa069ffc5689d856))
* **phase-10.3:** add saved replies for comment reuse ([b65ab11](https://github.com/mkappworks-dev/cloudzilla-app/commit/b65ab11c1154fe756f95b97795050502653ab15b))
* **phase-10.3:** add SavedReply service to manage saved replies ([01f6af5](https://github.com/mkappworks-dev/cloudzilla-app/commit/01f6af50b35ec0cc4ba55d92be2fe092ed6b1156))
* **phase-11.1:** add SMTP email notifications with digest preferences ([4fed3d8](https://github.com/mkappworks-dev/cloudzilla-app/commit/4fed3d86a28b26fb261afda139fe6883562fbeb8))
* **phase-11.2:** add OAuth 2.0 app registration and authorization code flow ([1bc503a](https://github.com/mkappworks-dev/cloudzilla-app/commit/1bc503a1ffd738d997bf8340ac1bfc5082a6b389))
* **phase-11.3:** add webhook retry backoff, redeliver, and event filter editing ([02e4c07](https://github.com/mkappworks-dev/cloudzilla-app/commit/02e4c07f106e4894fef288b9175c0c9d242276ce))
* **phase-12.1:** add repository watch levels and notification fan-out ([15603c2](https://github.com/mkappworks-dev/cloudzilla-app/commit/15603c26863b51ec2ecaaa297ba2293d16273f75))
* **phase-12.2:** add events table and personalised activity feed ([6b71c7e](https://github.com/mkappworks-dev/cloudzilla-app/commit/6b71c7e45067fd644c52fbaab8c78f353de3a0cd))
* **phase-12.3:** add categorised threaded discussions with accepted answers ([4c2b7ef](https://github.com/mkappworks-dev/cloudzilla-app/commit/4c2b7efe0c76712c658cc5c49ea0b23b12d25521))
* **phase-13.1:** add GitHub-style Gists feature ([00bd2fc](https://github.com/mkappworks-dev/cloudzilla-app/commit/00bd2fc6465807038b8ba868bf3aaa3a78c0472d))
* **phase-13.1:** mark Gists feature as completed in roadmap ([04690ef](https://github.com/mkappworks-dev/cloudzilla-app/commit/04690ef15908a3839b70d6aa65b773b3c4d3036a))
* **phase-13.2:** add GitHub-style Profile README feature ([61047da](https://github.com/mkappworks-dev/cloudzilla-app/commit/61047da85e891e474908e55376780d243b9ef6c0))
* **phase-13.3:** add repository topics feature ([3d34041](https://github.com/mkappworks-dev/cloudzilla-app/commit/3d34041c78c6fad82f35e48772d779ea6a359099))
* **phase-14.1:** add private issues ([73a85df](https://github.com/mkappworks-dev/cloudzilla-app/commit/73a85dff5ae415460b2b351e91ee83027ca53ff3))
* **phase-14.2:** add archive and template repositories ([3e4a9dc](https://github.com/mkappworks-dev/cloudzilla-app/commit/3e4a9dc8c9488b9a16565642723f1282bca6b5f8))
* **phase-14.3:** add repository soft-delete and recovery ([c8cc0d0](https://github.com/mkappworks-dev/cloudzilla-app/commit/c8cc0d03f94a94ace391477bb4573bf286036b7c))
* **phase-15.1:** add advanced code search with PostgreSQL FTS indexing ([4aa4eba](https://github.com/mkappworks-dev/cloudzilla-app/commit/4aa4eba72d6c8252fcc81b03c6d40de940992217))
* **phase-15.2:** add explore/trending page with three repo views ([32d4438](https://github.com/mkappworks-dev/cloudzilla-app/commit/32d4438be519081e687147f7475877b35f8b9bff))
* **phase-15.3:** add dependency graph with manifest parsing and network/dependencies page ([5f05eb5](https://github.com/mkappworks-dev/cloudzilla-app/commit/5f05eb566554bf7eb071f78aaf2faaf13f4a488e))
* **pr:** add fast-forward merge and diff view for pull requests ([59d7db6](https://github.com/mkappworks-dev/cloudzilla-app/commit/59d7db6336653c3b3c31cd09acef2647d562e991))
* **pr:** add three-way and squash merge strategies ([2806691](https://github.com/mkappworks-dev/cloudzilla-app/commit/28066916037a4ff98b3d14a7d04ae61e46f93af2))
* **projects:** add Phase 9.1 Kanban project boards ([f353aa7](https://github.com/mkappworks-dev/cloudzilla-app/commit/f353aa7d73ae254286e714ebdb4dab613191b8b0))
* **pull:** implement Draft Pull Requests (Phase 5.3) ([4cf6571](https://github.com/mkappworks-dev/cloudzilla-app/commit/4cf65717dd828dc31820242a3bb7d1b9694641c7))
* **reactions:** add handler, routes, fragment, and issue_detail integration (Phase 7.3) ([b0f68c9](https://github.com/mkappworks-dev/cloudzilla-app/commit/b0f68c98b45a5c9bd096b9ec7de9c1e520cca4d0))
* **reactions:** add model, store, service, and wiring (Phase 7.3) ([617b79a](https://github.com/mkappworks-dev/cloudzilla-app/commit/617b79a89db0f74a833dc184e8e46528b486b53a))
* **router:** register refs page and branch/tag API routes ([5cbd523](https://github.com/mkappworks-dev/cloudzilla-app/commit/5cbd523fc244f5093b2a880bcb0ba949d60e5a82))
* **run-phases:** add documentation update step and refine implementation instructions ([84739dd](https://github.com/mkappworks-dev/cloudzilla-app/commit/84739ddab64e5ad9df25e91d04682d93965b7965))
* **run-phases:** enhance phase implementation with silent failure, code, and security reviews ([7c15e31](https://github.com/mkappworks-dev/cloudzilla-app/commit/7c15e310fa5af0785f28c6a883eef8841eea8240))
* **run-phases:** streamline phase review process with structured prompts and remove redundant logging ([d019849](https://github.com/mkappworks-dev/cloudzilla-app/commit/d019849409efb3f1553d78caa337723ee7616e5e))
* **security:** private-repo visibility + transport + UI polish ([#29](https://github.com/mkappworks-dev/cloudzilla-app/issues/29)) ([96a6f51](https://github.com/mkappworks-dev/cloudzilla-app/commit/96a6f51dd65e54d1b9b004f5cb06490582a02640))
* **service:** add branch and tag management to CodeService ([9c47ef1](https://github.com/mkappworks-dev/cloudzilla-app/commit/9c47ef118ebf2b69cf12db638db3c725cd42a73d))
* **service:** add EnableAutoMerge, DisableAutoMerge, ListOpen, GetByID to PullService ([1ada537](https://github.com/mkappworks-dev/cloudzilla-app/commit/1ada537c84f5533adcae29a4a3003da57f336d83))
* **service:** add Fork method and enrichForkInfo to RepoService ([d099765](https://github.com/mkappworks-dev/cloudzilla-app/commit/d099765a2ed097f6edbeb7ba9b71031e69fad2de))
* **service:** add release, commit status, and milestone services ([f9fb6b2](https://github.com/mkappworks-dev/cloudzilla-app/commit/f9fb6b2c010f9a69b05159aa770f87908b8a5ed0))
* **settings:** add instance access controls and admin panel ([ae26488](https://github.com/mkappworks-dev/cloudzilla-app/commit/ae264882b3800c21f61eae4e1ca4ff9776701c1d))
* **setup:** add first-run wizard and superadmin role ([de7bb07](https://github.com/mkappworks-dev/cloudzilla-app/commit/de7bb077a9ecd3a49225a0205093b2ecc977df56))
* **stars:** add star store, service, and handler ([067f18b](https://github.com/mkappworks-dev/cloudzilla-app/commit/067f18b09c37c90f83c52419e71283bcac1f72d2))
* **stars:** add stargazers and user starred repos page templates ([9d4628b](https://github.com/mkappworks-dev/cloudzilla-app/commit/9d4628b1e32aa4c7fd7829544e8af695c883dc5b))
* **store,service:** implement full-text search logic ([75345e0](https://github.com/mkappworks-dev/cloudzilla-app/commit/75345e0fd7c6c78c711e0a09eb4c4968c35dbf64))
* **store:** add release, commit status, and milestone stores ([8dd434c](https://github.com/mkappworks-dev/cloudzilla-app/commit/8dd434c19060c9ad131c575bfdd90c2289253bfa))
* **store:** add SetAutoMerge, ListOpen, GetByID to PullStore ([7e3bc57](https://github.com/mkappworks-dev/cloudzilla-app/commit/7e3bc575c742885167af58d9a219d502b564c576))
* **store:** migrate query layer from SQLite to PostgreSQL ([5b79f01](https://github.com/mkappworks-dev/cloudzilla-app/commit/5b79f01989fe72cce24437cd0efd61cbb7ef3f47))
* **suggestions:** implement Phase 6.3 Code Review Suggestions ([9abf43b](https://github.com/mkappworks-dev/cloudzilla-app/commit/9abf43b9289563842a0b3d9846d3493a14a62e2c))
* surface Register button and quick links so top-level pages are reachable ([854fc18](https://github.com/mkappworks-dev/cloudzilla-app/commit/854fc188e675e5ec1f73ec7414a51c63881b65c3))
* **tailwind:** add @tailwindcss/typography plugin ([0049030](https://github.com/mkappworks-dev/cloudzilla-app/commit/00490302f2f103bb8e272f69c427c01151766fe2))
* **templates:** add HTMX fragment templates for labels, assignees, and stars ([9e2754b](https://github.com/mkappworks-dev/cloudzilla-app/commit/9e2754b58e71daf67389d062ec070f5597038e8f))
* **templates:** add issue_new.html and pull_new.html (Phase 7.2) ([361dbea](https://github.com/mkappworks-dev/cloudzilla-app/commit/361dbeaf727ad135f3efbbfe27ab028bd854b338))
* **templates:** add viewmodels, handlers, and routes for new issue/PR pages ([26df66d](https://github.com/mkappworks-dev/cloudzilla-app/commit/26df66d8dce96939e2c8fd086825485334df29a3))
* **ui:** add Branches & Tags nav card and clickable ref badges ([b5bf630](https://github.com/mkappworks-dev/cloudzilla-app/commit/b5bf630bde8254ac35ef7e1b35ab8ba276750957))
* **ui:** add Enable/Disable auto-merge UI to pull detail fragment ([8181949](https://github.com/mkappworks-dev/cloudzilla-app/commit/81819493bf4d7ef3546ab3cdf7001c2dfbeb45b8))
* **ui:** add refs page template and branch/tag HTMX fragments ([8cb6749](https://github.com/mkappworks-dev/cloudzilla-app/commit/8cb6749f359fc436c11030863ce8d3fa3aa6c89e))
* upgrade admin role with CanManage, add IsOwner for destructive ops ([c95a000](https://github.com/mkappworks-dev/cloudzilla-app/commit/c95a0006b68fc04266729f345bf2142891b1f01d))
* **view:** add view helpers and layout templ component ([b603d14](https://github.com/mkappworks-dev/cloudzilla-app/commit/b603d1417bc5783979b8c547fb736772f8399286))
* **view:** convert comment, comments, reactions, pr_reviews, line_comments fragments to templ ([19eb4f9](https://github.com/mkappworks-dev/cloudzilla-app/commit/19eb4f987c146abcfe78b9c09b91f8197f3f2271))
* **view:** convert home, user, org, settings, notifications, search pages to templ ([4131336](https://github.com/mkappworks-dev/cloudzilla-app/commit/4131336968ba8ff4288c00f8e8b9b80e03caa493))
* **view:** convert issue/pull sidebar, fork button, star button, milestone fragments to templ ([fd59b3a](https://github.com/mkappworks-dev/cloudzilla-app/commit/fd59b3a26cdb7f098a193ebca6bcb9ec7d45257f))
* **view:** convert issues, pulls, issue_detail, pull_detail, issue_new, pull_new pages to templ ([9e184c9](https://github.com/mkappworks-dev/cloudzilla-app/commit/9e184c9643de753b2240eff5785bee8c89dee0ce))
* **view:** convert login, setup, invite pages to templ ([de1cbc1](https://github.com/mkappworks-dev/cloudzilla-app/commit/de1cbc1d4ee40b8e7730926591af46c7d1276c1d))
* **view:** convert milestones, releases, release_detail, admin_settings pages to templ ([6eadf4b](https://github.com/mkappworks-dev/cloudzilla-app/commit/6eadf4b95aa5ad9196ae5b7ffe2f50c86bd16319))
* **view:** convert remaining settings/admin/notifications fragments to templ ([f3ef729](https://github.com/mkappworks-dev/cloudzilla-app/commit/f3ef729d06afec892f8f9aa460b5c2205eb2eb04))
* **view:** convert repo, repo_settings, stargazers, user_stars pages to templ ([89b8c19](https://github.com/mkappworks-dev/cloudzilla-app/commit/89b8c19f3470168fc69ef8ef9f117b75f86dd7af))
* **view:** convert tree, blob, blame, commits, commit, refs pages to templ ([a927859](https://github.com/mkappworks-dev/cloudzilla-app/commit/a92785974344de2db45850cfa61ea72bbbaf02ed))
* **viewmodel:** add auto-merge fields to PullDetailData and PullDetailFragData ([0d8283a](https://github.com/mkappworks-dev/cloudzilla-app/commit/0d8283af20e3e26bd26dfcede06f1173fcc92004))
* **webhook:** add per-repo webhooks ([0cd18e3](https://github.com/mkappworks-dev/cloudzilla-app/commit/0cd18e34a93413d7d01ac63f112f0dffc52047c4))
* **wiki:** add Phase 9.2 git-backed repository wiki ([7d66a33](https://github.com/mkappworks-dev/cloudzilla-app/commit/7d66a336b9de25339b026a76f56e9c772f982d87))
* wire organizations, webhooks, and notifications ([63a5fe6](https://github.com/mkappworks-dev/cloudzilla-app/commit/63a5fe6e1f502e4989bb28eaf93ea6735067fd23))
* **wiring:** register label, assignee, and star stores and services ([747ab8c](https://github.com/mkappworks-dev/cloudzilla-app/commit/747ab8c70ad5412916c88740a4290b718c426a85))


### Bug Fixes

* add IsOwner check to TransferRepo, validate inputs, deduplicate name regex ([903070a](https://github.com/mkappworks-dev/cloudzilla-app/commit/903070ad22d75be5eadebd9446de9b3ff7baf560))
* address code review findings from chunk 5 ([3353b22](https://github.com/mkappworks-dev/cloudzilla-app/commit/3353b2214fdda0a46b6015cfb5822855e75c82bb))
* address security findings from chunk 5 code review ([7376e79](https://github.com/mkappworks-dev/cloudzilla-app/commit/7376e796e359ce7973af364e116a4765426065ef))
* **audit:** address security review findings (Phase 8.2) ([830d9cc](https://github.com/mkappworks-dev/cloudzilla-app/commit/830d9cc7f5d7023012e953aae9c3701d123f511f))
* **config:** add Viper env key replacer so CZ_DATABASE_DSN maps correctly ([e528b53](https://github.com/mkappworks-dev/cloudzilla-app/commit/e528b53817b4d85c480e4a9ca7d1e384bfb5682b))
* correct HTMX target for SSH key deletion ([6d65723](https://github.com/mkappworks-dev/cloudzilla-app/commit/6d657236a0b79e18faeb876a413fb02ad1a9f100))
* **deps:** update go-git to v5.17.2 ([91a947b](https://github.com/mkappworks-dev/cloudzilla-app/commit/91a947babf233e2e65761a18c94f87515137f2ea))
* **docker:** pin Tailwind v3.4.19, bump Go base image to 1.26, fix curl flags ([f87af08](https://github.com/mkappworks-dev/cloudzilla-app/commit/f87af08c15813add1939d18e45f3704fdf3fbe5a))
* **handler:** add comment edit endpoint and enforce delete ownership ([81527df](https://github.com/mkappworks-dev/cloudzilla-app/commit/81527df24adf877bc2fdcdf9945b4949904d0ff8))
* **handler:** add PR line comment edit endpoint ([4b74dca](https://github.com/mkappworks-dev/cloudzilla-app/commit/4b74dcacbe4edbc198f12e7a6b798cd9eaa123bb))
* include CSRF token in hand-rolled fetch() calls ([55d86e4](https://github.com/mkappworks-dev/cloudzilla-app/commit/55d86e4b429c6c44daf14f1d16c56cf88d58b6c3))
* **migrations:** consolidate into internal/db/migrations, rewrite for PostgreSQL ([de9a718](https://github.com/mkappworks-dev/cloudzilla-app/commit/de9a718ac358d16f62a212a30c79fd6a5742cc99))
* **phase-10.1:** address review findings ([92bce8c](https://github.com/mkappworks-dev/cloudzilla-app/commit/92bce8cb743baa707e23187748c12cfa0b3a81d6))
* **phase-10.2:** address review findings ([ce197f7](https://github.com/mkappworks-dev/cloudzilla-app/commit/ce197f72aea6e83c5cd3f1bfbad625d7427065ed))
* **phase-10.3:** address review findings ([9c156a2](https://github.com/mkappworks-dev/cloudzilla-app/commit/9c156a2d6840dfd74f96ffa5f55c60a14fc30b9f))
* **phase-11.1:** address review findings ([8e86e61](https://github.com/mkappworks-dev/cloudzilla-app/commit/8e86e611247a024071242c862ef802bb07e2db28))
* **phase-11.2:** address review findings ([f676cc7](https://github.com/mkappworks-dev/cloudzilla-app/commit/f676cc701c54c822a86898035e50c040b13320b8))
* **phase-11.3:** address review findings ([d759ec5](https://github.com/mkappworks-dev/cloudzilla-app/commit/d759ec51b4f8b68ae3abf255fa68e0ea7eedd154))
* **phase-12.1:** address review findings ([c271f9f](https://github.com/mkappworks-dev/cloudzilla-app/commit/c271f9fac0f81cb367b48e04fd22bfc343e902c7))
* **phase-12.1:** address security review findings ([83f8d10](https://github.com/mkappworks-dev/cloudzilla-app/commit/83f8d10f19e34b6c0883890cd5e81438e1430ad3))
* **phase-12.2:** address review findings ([d1a6fd2](https://github.com/mkappworks-dev/cloudzilla-app/commit/d1a6fd2a3bc49b5ad4f96ea76473cd5fa4c45c19))
* **phase-12.3:** address review findings ([45f63bf](https://github.com/mkappworks-dev/cloudzilla-app/commit/45f63bfa07c032ab4750fa2f0f38aea8101b103d))
* **phase-13.1:** address review findings ([c8ae605](https://github.com/mkappworks-dev/cloudzilla-app/commit/c8ae605097a5ddcf5df64a1e698f14f81f51f0f4))
* **phase-13.2:** address review findings ([21ccc23](https://github.com/mkappworks-dev/cloudzilla-app/commit/21ccc23fa56e59b392353340d0164f1caf4c9890))
* **phase-14.1:** address review findings ([4edcfc3](https://github.com/mkappworks-dev/cloudzilla-app/commit/4edcfc3a2427db3eecbf7fcb50ab309c202b0832))
* **phase-14.2:** address review findings ([3e4a9dc](https://github.com/mkappworks-dev/cloudzilla-app/commit/3e4a9dc8c9488b9a16565642723f1282bca6b5f8))
* **phase-14.3:** address review findings ([00c3cbf](https://github.com/mkappworks-dev/cloudzilla-app/commit/00c3cbf996b7a4207cef79fd6353a3042f27f5b9))
* **phase-15.1:** address review findings ([0b12ed2](https://github.com/mkappworks-dev/cloudzilla-app/commit/0b12ed2fed02400a1a7112aaae2d7436f76a0f1b))
* **phase-15.2:** address review findings ([2937437](https://github.com/mkappworks-dev/cloudzilla-app/commit/293743775b1427afdf0061087bd3a20e4fcddd71))
* **phase-15.3:** address review findings ([9540029](https://github.com/mkappworks-dev/cloudzilla-app/commit/9540029452881d5af71e10a3f9ece0b7ef287400))
* **phase-9.2:** address review findings ([8a7fe31](https://github.com/mkappworks-dev/cloudzilla-app/commit/8a7fe319ce2cebee1638ccf04098255bfae2ba9e))
* **phase-9.3:** address review findings ([6b5d625](https://github.com/mkappworks-dev/cloudzilla-app/commit/6b5d6257742c60f9b1a83471823e2efe445a7dc4))
* **projects:** harden security and fix cross-project data isolation bugs ([d783c4f](https://github.com/mkappworks-dev/cloudzilla-app/commit/d783c4f7ab347d06c415cb3a5d66f2f5e7b69c6d))
* **reactions:** validate repo ownership + correct error status codes ([ef7b3e5](https://github.com/mkappworks-dev/cloudzilla-app/commit/ef7b3e5d997bb8b0dd29bf33eb4541350b6511cb))
* remove push trigger from CI workflow, keeping only pull request trigger ([fd634c9](https://github.com/mkappworks-dev/cloudzilla-app/commit/fd634c91a2d085ee4ba7443de5ae27f4c9d3e925))
* **run-phases:** simplify next steps instructions after phase review ([193362c](https://github.com/mkappworks-dev/cloudzilla-app/commit/193362cce9f9a6365868367a02273bf4f6dbeca3))
* **sso:** address security review findings (Phase 8.3) ([d641be5](https://github.com/mkappworks-dev/cloudzilla-app/commit/d641be5970a905bf766c572d6bb467ae4108beda))
* **store,model:** add author_name column to comments table ([e6eb68b](https://github.com/mkappworks-dev/cloudzilla-app/commit/e6eb68b75055c170f2295a0ada13df6da9342387))
* **templates:** disable VS Code HTML formatter for HTML files ([95e246e](https://github.com/mkappworks-dev/cloudzilla-app/commit/95e246efa3b2b04515d173a58d28c58677bf4fe6))
* **transport:** implement full git pack protocol for HTTP and SSH ([4b29b52](https://github.com/mkappworks-dev/cloudzilla-app/commit/4b29b5255c331f16e7bcda9f05e25e23232f27a1))
* update dependencies in go.mod and go.sum for compatibility and security ([c0ca250](https://github.com/mkappworks-dev/cloudzilla-app/commit/c0ca250cfa94d3b770c0928a3e8aae05b4b9d5b9))
* update GitHub issue link for vulnerability reporting ([8d41233](https://github.com/mkappworks-dev/cloudzilla-app/commit/8d412338a46a47f5fe7b887169afb1c82063b9a8))
* update release-please configuration for pull request handling ([a5a6258](https://github.com/mkappworks-dev/cloudzilla-app/commit/a5a6258034d1449be807074da7316fe3d17fe8fe))
* **wiki:** address security findings from silent-failure review ([80b6b8b](https://github.com/mkappworks-dev/cloudzilla-app/commit/80b6b8bd1f92b7057723af1a4a1070e11090209f))


### Refactors

* align handlers and views with new permission model ([9a41160](https://github.com/mkappworks-dev/cloudzilla-app/commit/9a41160c25be05670821911f7d2e82ff5dba5aaf))
* **handler:** replace html/template with templ component rendering ([4c3f831](https://github.com/mkappworks-dev/cloudzilla-app/commit/4c3f831c5a17c9389a806dac74f9918b69a7022f))
* remove sqlc, standardize all stores on direct sqlx queries ([e86fc13](https://github.com/mkappworks-dev/cloudzilla-app/commit/e86fc138a2cce101d1f5ad1798538aacc524f70f))
* remove SQLite support, standardize on PostgreSQL only ([6ad536a](https://github.com/mkappworks-dev/cloudzilla-app/commit/6ad536ab3ac3305629755e274934c0ba3d0c0570))
* replace fmt.Fprintf with renderFragment in HTMX handlers ([0b02839](https://github.com/mkappworks-dev/cloudzilla-app/commit/0b02839f59d5417f9cf8e8b247d4988a183ce2de))
* **router:** add template parsing and page routes ([3c626cf](https://github.com/mkappworks-dev/cloudzilla-app/commit/3c626cfa10a62c6421a56b0ecc23152799782376))
* **run-phases:** streamline implementation and review steps ([1da1cf7](https://github.com/mkappworks-dev/cloudzilla-app/commit/1da1cf7b3fffda4c0306401eec64157b9b2d5cce))
* split code_service.go, page_handler.go, and viewmodels.go into focused domain files ([3b191c3](https://github.com/mkappworks-dev/cloudzilla-app/commit/3b191c358ad1656bffad8ad5810898fb2f1d79d9))
* use switch statement in GetCommit diff line counting ([9c0508a](https://github.com/mkappworks-dev/cloudzilla-app/commit/9c0508ab5cdab22c51b9e62c56b29f557a8de2d2))
* use unconditional strings.TrimSuffix in git_http.go ([65dc68c](https://github.com/mkappworks-dev/cloudzilla-app/commit/65dc68cd9798a9a170570feaefc3ee4a27042dec))
* **view:** move viewmodels to internal/view, change BodyHTML to string ([b47c189](https://github.com/mkappworks-dev/cloudzilla-app/commit/b47c18964166088c6e54de2c9907142b482ec294))


### Documentation

* add alpha status notice ([31b4f4c](https://github.com/mkappworks-dev/cloudzilla-app/commit/31b4f4c58fdad2b579199c66cfda60aacd603c69))
* add color to Mermaid diagrams and ASCII art equivalents ([e570d78](https://github.com/mkappworks-dev/cloudzilla-app/commit/e570d78ad967225486f8aa0284df8db1f4651cc1))
* add GoDoc comments to all exported symbols across the codebase ([9d9a576](https://github.com/mkappworks-dev/cloudzilla-app/commit/9d9a5762484e29f32834014ac15af422e915c6cd))
* add implementation plans for phases 8.1–15.3 and update roadmap ([d114e06](https://github.com/mkappworks-dev/cloudzilla-app/commit/d114e06115a270de82cdcab1b94d2f320abefd83))
* add security review + PR workflow to CLAUDE.md and all phase plans (8.2–15.3) ([16e74ed](https://github.com/mkappworks-dev/cloudzilla-app/commit/16e74ed78a259cad3b627e84629d9a77923eb3e5))
* **claude:** update developer guide for PostgreSQL ([310ad4a](https://github.com/mkappworks-dev/cloudzilla-app/commit/310ad4a046f94b428f335992e4f67d291b5e2a2b))
* comprehensive auth, authorization, roles and permissions guide ([a2df86c](https://github.com/mkappworks-dev/cloudzilla-app/commit/a2df86c7165eb3e8629a0e6ef84640e011583675))
* document branch and tag management feature ([9f3f7ef](https://github.com/mkappworks-dev/cloudzilla-app/commit/9f3f7ef8994972bb3dacd835faa96d542ef32585))
* document Docker deployment in CLAUDE.md and README.md ([944bd37](https://github.com/mkappworks-dev/cloudzilla-app/commit/944bd371f96cd6c90d6a3d6a9beb720d70f2b9bb))
* document Google OAuth, SSH host key setup, and production deployment ([7c13f8f](https://github.com/mkappworks-dev/cloudzilla-app/commit/7c13f8ff62c421d12db8921a288960a07abc538e))
* document organizations, webhooks, and notifications ([a2d100e](https://github.com/mkappworks-dev/cloudzilla-app/commit/a2d100e5712e537453888a391ad4f09bddd4fafc))
* document PR fast-forward merge, diff view, and dependency upgrades ([c9f9112](https://github.com/mkappworks-dev/cloudzilla-app/commit/c9f9112c7c504310de1d1ab8a492e04b6efc78cf))
* document superadmin, instance settings, invitations, and collaborators ([de9b0c2](https://github.com/mkappworks-dev/cloudzilla-app/commit/de9b0c2c352a942451f057297a840eb452df45a4))
* document three-way and squash merge strategies ([45d3525](https://github.com/mkappworks-dev/cloudzilla-app/commit/45d3525b4b648101d90def2d0c637eb4c1350089))
* extend chunk-6 plan with deep audit findings (530+ untested methods) ([7bfda55](https://github.com/mkappworks-dev/cloudzilla-app/commit/7bfda553140ac99377b0478fdb283916c5b2078d))
* fix 10 permission discrepancies in access-control.md and CLAUDE.md ([a1ada23](https://github.com/mkappworks-dev/cloudzilla-app/commit/a1ada2355de963ee78720de276ed8abfa34ce094))
* mark chunk 6 test coverage plan as complete ([3a2c31d](https://github.com/mkappworks-dev/cloudzilla-app/commit/3a2c31da3b886301ec8767e2968080b2756d019f))
* mark chunk 6 test coverage plan as in progress ([696f08c](https://github.com/mkappworks-dev/cloudzilla-app/commit/696f08c76304b90d4433e7ed85beec0f33b7d933))
* mark Phase 7.1 Auto-merge as done in README, CLAUDE.md, roadmap ([377d214](https://github.com/mkappworks-dev/cloudzilla-app/commit/377d214ba65a0700c77c113701d0a0b007cea121))
* mark Phase 7.1 Auto-merge as done in ROADMAP.md ([893d5de](https://github.com/mkappworks-dev/cloudzilla-app/commit/893d5de13b971555bb41e8f721933023a2c5b2f2))
* move detailed roadmap specs to docs/ROADMAP.md ([b7ad36c](https://github.com/mkappworks-dev/cloudzilla-app/commit/b7ad36c93ae448ad99bf7a465df3d89f7e73daa3))
* **phase-10.1:** update readme, claude.md, and roadmap ([74f97ee](https://github.com/mkappworks-dev/cloudzilla-app/commit/74f97eee95ffbdc7280f8c08977bf272ffb93227))
* **phase-10.1:** update roadmap to mark Repository Insights & Stats as done ([dd85b15](https://github.com/mkappworks-dev/cloudzilla-app/commit/dd85b1585b2ece92a415f0557efcecf18e6290a5))
* **phase-10.2:** update readme, claude.md, and roadmap ([988bd26](https://github.com/mkappworks-dev/cloudzilla-app/commit/988bd2664a12361febda9b7741787e17ed7bcdf7))
* **phase-10.2:** update roadmap to mark @Mentions in Comments as done ([226b558](https://github.com/mkappworks-dev/cloudzilla-app/commit/226b5588635000fe1452df416d908c60e3529e04))
* **phase-10.3:** update readme, claude.md, and roadmap ([8a1e545](https://github.com/mkappworks-dev/cloudzilla-app/commit/8a1e5453d371810da4263b696d32f94c7ab08770))
* **phase-10.3:** update roadmap to mark Saved Replies as completed ([483c1ad](https://github.com/mkappworks-dev/cloudzilla-app/commit/483c1ada8754af62e6474e06e2c6d503e66b9e40))
* **phase-11.1:** update readme, claude.md, and roadmap ([e7fcdb8](https://github.com/mkappworks-dev/cloudzilla-app/commit/e7fcdb811ae6f1ca4473206b715d11993d18c863))
* **phase-11.1:** update roadmap to mark Email Notifications as completed ([15a0150](https://github.com/mkappworks-dev/cloudzilla-app/commit/15a0150c792b0de25e59ce22fefedd631c45ee79))
* **phase-11.2:** mark phase 11.2 as completed in roadmap ([98e8b33](https://github.com/mkappworks-dev/cloudzilla-app/commit/98e8b3399b65e4b5e9cfe039924201e2f2c57db8))
* **phase-11.2:** update readme, claude.md, and roadmap ([90d4212](https://github.com/mkappworks-dev/cloudzilla-app/commit/90d42128053c4f45ce3d72eb458023061b6b98f5))
* **phase-11.3:** update readme, claude.md, and roadmap ([102b665](https://github.com/mkappworks-dev/cloudzilla-app/commit/102b6651d58563a7880cf56768c581fce8ccc54c))
* **phase-12.1:** update readme, claude.md, and roadmap ([4230054](https://github.com/mkappworks-dev/cloudzilla-app/commit/423005422affccaae838c0a1ddaa3327a4b6621a))
* **phase-12.1:** update roadmap ([5045c06](https://github.com/mkappworks-dev/cloudzilla-app/commit/5045c06ffbc2515017d950b82b4543ffbbff4856))
* **phase-12.2:** update readme, claude.md, and roadmap ([33649d9](https://github.com/mkappworks-dev/cloudzilla-app/commit/33649d9c6ed69c6728eb7335ee97fc40fa4c1a06))
* **phase-12.2:** update roadmap ([488c058](https://github.com/mkappworks-dev/cloudzilla-app/commit/488c058db664062795d0eb6f55c4e76a273d0d87))
* **phase-12.3:** update readme, claude.md, and roadmap ([b6ffaa3](https://github.com/mkappworks-dev/cloudzilla-app/commit/b6ffaa3d31e4253b82e35566c54c19561f0d08e3))
* **phase-12.3:** update roadmap ([28f195a](https://github.com/mkappworks-dev/cloudzilla-app/commit/28f195aaf7e0298eae190baaffcfbd18022e23c2))
* **phase-13.1:** update readme, claude.md, and roadmap ([eec43ab](https://github.com/mkappworks-dev/cloudzilla-app/commit/eec43ab78829494beca052f2572a99ef7240ff6f))
* **phase-13.2:** update readme, claude.md, and roadmap ([e432d51](https://github.com/mkappworks-dev/cloudzilla-app/commit/e432d51a5b6875542471863bcbe3f8b29063d4fd))
* **phase-13.3:** update readme, claude.md, and roadmap ([260f9c7](https://github.com/mkappworks-dev/cloudzilla-app/commit/260f9c7bb1275e640f3c97533543fc580221627d))
* **phase-14.1:** sync ROADMAP.md case-variant ([5f24b99](https://github.com/mkappworks-dev/cloudzilla-app/commit/5f24b99f1b3494848eca1a6d0d42b6c72383cd09))
* **phase-14.1:** update readme, claude.md, and roadmap ([ffd3823](https://github.com/mkappworks-dev/cloudzilla-app/commit/ffd3823c2364b4dccdbdef201ca529a640e8892c))
* **phase-14.2:** update readme, claude.md, and roadmap ([d6e32b0](https://github.com/mkappworks-dev/cloudzilla-app/commit/d6e32b09ea0461ce63b4f32f15bf90700a65ffc1))
* **phase-14.3:** update readme, claude.md, and roadmap ([2f2c2eb](https://github.com/mkappworks-dev/cloudzilla-app/commit/2f2c2eb8dec485966edb4a384a192a02b65790ed))
* **phase-14.3:** update roadmap ([40c1cf4](https://github.com/mkappworks-dev/cloudzilla-app/commit/40c1cf4202c41536f1d8bc9ec80292c015c16151))
* **phase-15.1:** mark phase 15.1 as Done in ROADMAP.md ([57159c6](https://github.com/mkappworks-dev/cloudzilla-app/commit/57159c643433637157864d3c99a03942f4b261c8))
* **phase-15.1:** update readme, claude.md, and roadmap ([56706b7](https://github.com/mkappworks-dev/cloudzilla-app/commit/56706b7f89d76a25ba22519c01cac005d3c6162a))
* **phase-15.2:** mark Explore / Trending as Done in ROADMAP.md ([f5f9ea3](https://github.com/mkappworks-dev/cloudzilla-app/commit/f5f9ea39c049331ebe3a41f6cd83911f5b200fa0))
* **phase-15.2:** update readme, claude.md, and roadmap ([6c16024](https://github.com/mkappworks-dev/cloudzilla-app/commit/6c160249f07086eb5f6f593d14f7a3f843df10ab))
* **phase-15.3:** update readme, claude.md, and roadmap ([4e673ad](https://github.com/mkappworks-dev/cloudzilla-app/commit/4e673ad58e758cb637fc759919e1238e52189425))
* **phase-15.3:** update roadmap ([d8dcc4c](https://github.com/mkappworks-dev/cloudzilla-app/commit/d8dcc4c8223c725d166f79e1a7ce27fc728b608b))
* **phase-9.2:** update readme, claude.md, and roadmap ([969949f](https://github.com/mkappworks-dev/cloudzilla-app/commit/969949f0e413d4f963ec22f21f22f8a04e453012))
* **phase-9.2:** update roadmap status and WikiPageGet signature ([47e9c26](https://github.com/mkappworks-dev/cloudzilla-app/commit/47e9c269d138557b0a92e3676202876462a0a3b7))
* **phase-9.3:** mark 8.2 and 8.3 as done in CLAUDE.md ([ef870b3](https://github.com/mkappworks-dev/cloudzilla-app/commit/ef870b3608625c53dd762007664f6c409be600cf))
* **phase-9.3:** mark 9.3 done in roadmap ([4e95719](https://github.com/mkappworks-dev/cloudzilla-app/commit/4e957199351b0ebc820a94a13fb15af6d40f69a7))
* **phase-9.3:** update readme, claude.md, and roadmap ([f78088f](https://github.com/mkappworks-dev/cloudzilla-app/commit/f78088f8da2346acc20d5b44795db06aac48020d))
* **plans:** fix migration numbers and remove make migrate steps for phases 11.1-15.3 ([d72cc16](https://github.com/mkappworks-dev/cloudzilla-app/commit/d72cc1696ec3301f619315c4ec9b46b16d97ea67))
* **plans:** update all phase plans to templ conventions ([235ec6f](https://github.com/mkappworks-dev/cloudzilla-app/commit/235ec6f93b1a0757bae048f5910b7cc86a01966e))
* refactor CLAUDE.md into focused subsystem reference files ([10ce4e0](https://github.com/mkappworks-dev/cloudzilla-app/commit/10ce4e0863c3e6d4dafb561bddcd7a08103eb7d2))
* remove ASCII fallbacks and fix mermaid Permission Model ([3bc0349](https://github.com/mkappworks-dev/cloudzilla-app/commit/3bc03490dfb9b190eba8b6e346236351e9b0e947))
* simplify dependency health section ([985c586](https://github.com/mkappworks-dev/cloudzilla-app/commit/985c586060205104457fdad70dae9402f4dd2a81))
* slim README, add milestone structure, clean up old plans ([c4ddd94](https://github.com/mkappworks-dev/cloudzilla-app/commit/c4ddd949c16d3ff29914ab0a4df20e5749c2b3c5))
* update chunk plans with completion status and scope ([38cddcb](https://github.com/mkappworks-dev/cloudzilla-app/commit/38cddcb4f5551d5728db3d1539e72db770315032))
* update CLAUDE.md and README.md for Phase 2 repository fork ([a113a7d](https://github.com/mkappworks-dev/cloudzilla-app/commit/a113a7d386cc3d9672a23887c893f8f7f28e671c))
* update CLAUDE.md and README.md for Phase 3 (releases, commit statuses, milestones) ([a709cf9](https://github.com/mkappworks-dev/cloudzilla-app/commit/a709cf9fa803cca567bc5b2a824c5b66867186ba))
* update CLAUDE.md and README.md with Phase 1 implementation and full roadmap ([97eea0f](https://github.com/mkappworks-dev/cloudzilla-app/commit/97eea0f6f93ce120037a31311d5f541adb3bb592))
* update CLAUDE.md and related store/service for architecture ([5b184ed](https://github.com/mkappworks-dev/cloudzilla-app/commit/5b184ed73db382269cc87e9a249b6b300345e4c9))
* update CLAUDE.md, ROADMAP.md, and README.md for Phase 4 ([023961b](https://github.com/mkappworks-dev/cloudzilla-app/commit/023961bb77245383a7179f1234f6a615f8fbd2a5))
* update docs and README for Phase 5 (5.1–5.3) ([3a82cd0](https://github.com/mkappworks-dev/cloudzilla-app/commit/3a82cd0e9a13f58dfb17c79b370ddb83fe4c5c5e))
* update docs and README for Phase 6 (6.1–6.3) ([dc127e4](https://github.com/mkappworks-dev/cloudzilla-app/commit/dc127e4738ae8d7477cff43ce8153435e0e185e3))
* update for Go templates + Tailwind architecture ([ff2d82f](https://github.com/mkappworks-dev/cloudzilla-app/commit/ff2d82f79c1923d0eb8328290f64bbb177fdeb2b))
* update permission model docs for admin role and IsOwner ([2f75a20](https://github.com/mkappworks-dev/cloudzilla-app/commit/2f75a2018fd0c2804922e2a4388a2ddc5d9f175e))
* update README with SSH and Git HTTP/SSH documentation ([cce185f](https://github.com/mkappworks-dev/cloudzilla-app/commit/cce185f2c88d96a46b0c3807fcf51996055f30dd))
* update roadmap and README with phases 7.2–15.3 plan status ([a9a6ef6](https://github.com/mkappworks-dev/cloudzilla-app/commit/a9a6ef643718067433b10602ab71e1fc716f2786))
* update roadmap, README, and CLAUDE.md for phases 7–8.1 completion ([25c9604](https://github.com/mkappworks-dev/cloudzilla-app/commit/25c9604b673f68f9ac0b3067107f86c45db8ffef))
* update ROADMAP.md and README for Phase 4 bug fix audit ([8c5b1a7](https://github.com/mkappworks-dev/cloudzilla-app/commit/8c5b1a7f35dcc40a59644f1181c21c43b0543843))


### Build

* add Tailwind CSS configuration ([7e1745e](https://github.com/mkappworks-dev/cloudzilla-app/commit/7e1745e4216a714fbf694c656807bcf0a441b936))
* replace SolidStart with Tailwind, remove npm/Bun ([384b223](https://github.com/mkappworks-dev/cloudzilla-app/commit/384b2230f336da1a13cc5607dd65691cc25d3bd3))


### CI

* add GitHub Actions CI pipeline (test + build) ([27ae07e](https://github.com/mkappworks-dev/cloudzilla-app/commit/27ae07effcf378fff2d74db1efdc18075f7a4e26))
* set up release-please pipeline and branch protection scaffolding ([#30](https://github.com/mkappworks-dev/cloudzilla-app/issues/30)) ([2a5f3ec](https://github.com/mkappworks-dev/cloudzilla-app/commit/2a5f3ecb813182d24659ea9e7ce74a5b362a604d))


### Tests

* add P0 access token service tests (chunk 6) ([26c09d1](https://github.com/mkappworks-dev/cloudzilla-app/commit/26c09d16fc8bc46104631338a08cffbd871cff18))
* add P0 permission tests for RepoService (chunk 6) ([51127ca](https://github.com/mkappworks-dev/cloudzilla-app/commit/51127ca9cae56fd2801e5c6300ab9930360d206e))
* add P0 unit tests and test infrastructure (chunk 6) ([1b1f406](https://github.com/mkappworks-dev/cloudzilla-app/commit/1b1f406eaf4c350dd103db45e3d16548948da0fd))
* add P0/P1 test coverage with comments on all test functions ([14568c5](https://github.com/mkappworks-dev/cloudzilla-app/commit/14568c5eec4e86c64937e3f2b0b61cececcf1ff8))
* add P1 branch protection service tests (CheckPush + CheckMerge) ([c77acf9](https://github.com/mkappworks-dev/cloudzilla-app/commit/c77acf92d5e54bc7526f0be76783c5b10a129bf2))
* add tests for CSRF, claimsFromMap, SSRF protection, and ValidateName ([38b5cb8](https://github.com/mkappworks-dev/cloudzilla-app/commit/38b5cb85f7bf0c60036c391aa703312d89f378aa))
* add Wave 4 P3 breadth tests across store/service/handler layers ([25f7a50](https://github.com/mkappworks-dev/cloudzilla-app/commit/25f7a50352bf4d5a23334e78d88734fa5dab65fb))
* **handler:** add P3 integration tests for setup and issue handlers ([95b0df3](https://github.com/mkappworks-dev/cloudzilla-app/commit/95b0df3226c5b6c9e6ecc048f348f9c3b8013aa0))
* **handler:** add Wave 3 P2 handler integration tests ([9427fdb](https://github.com/mkappworks-dev/cloudzilla-app/commit/9427fdb500dd5e53294c64f5a8eeb6a0253eab91))
* pass context.TODO() instead of nil to satisfy SA1012 ([f435c65](https://github.com/mkappworks-dev/cloudzilla-app/commit/f435c658e58145514b2f540eaccc334e2864bf88))
* **service:** add P2 integration tests for Issue, Pull, Org, and OAuthApp services ([f36b808](https://github.com/mkappworks-dev/cloudzilla-app/commit/f36b808459a8b694153e0de6e2c5dbdee8b3d2f8))
* **service:** add Wave 1 P0 tests — user auth safety and notification suppression ([d2719a1](https://github.com/mkappworks-dev/cloudzilla-app/commit/d2719a19419b5fe28a0e3bfee8dac6ad498e4f04))
* **store/service:** add Wave 2 P1 tests — pull/org stores and comment/label/star services ([eccbbe7](https://github.com/mkappworks-dev/cloudzilla-app/commit/eccbbe7798429da458fdf0bd9b1edb196bec2548))
