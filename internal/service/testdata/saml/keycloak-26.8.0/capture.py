import base64, html, http.cookiejar, re, sys, urllib.parse, urllib.request, uuid, zlib

KC = "http://127.0.0.1:18480"
REALM = "cz-saml-test"
SP = "https://cz.test.invalid/auth/saml/metadata"
ACS = "https://cz.test.invalid/auth/saml/callback"
out = sys.argv[1]

authn = (
    '<samlp:AuthnRequest xmlns:samlp="urn:oasis:names:tc:SAML:2.0:protocol" '
    'xmlns:saml="urn:oasis:names:tc:SAML:2.0:assertion" '
    f'ID="_{uuid.uuid4().hex}" Version="2.0" IssueInstant="2026-10-10T00:00:00Z" '
    f'Destination="{KC}/realms/{REALM}/protocol/saml" '
    'ProtocolBinding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-POST" '
    f'AssertionConsumerServiceURL="{ACS}"><saml:Issuer>{SP}</saml:Issuer></samlp:AuthnRequest>'
)
c = zlib.compressobj(9, zlib.DEFLATED, -15)
req = base64.b64encode(c.compress(authn.encode()) + c.flush()).decode()

jar = http.cookiejar.CookieJar()
op = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(jar))
url = f"{KC}/realms/{REALM}/protocol/saml?" + urllib.parse.urlencode({"SAMLRequest": req, "RelayState": "/"})
page = op.open(url).read().decode()
for ck in jar:
    ck.secure = False  # KC marks cookies Secure even over http; browsers exempt loopback, urllib doesn't
action = html.unescape(re.search(r'<form[^>]*id="kc-form-login"[^>]*action="([^"]+)"', page).group(1))
body = urllib.parse.urlencode({"username": "ann", "password": "fixture-only-pw", "credentialId": ""}).encode()
page = op.open(urllib.request.Request(action, body)).read().decode()
resp = html.unescape(re.search(r'name="SAMLResponse"\s+value="([^"]+)"', page).group(1))
open(out, "wb").write(base64.b64decode(resp))
print("wrote", out)
