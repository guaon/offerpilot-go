import urllib.request, mimetypes, os

path = "D:\\Environment\\GoWorks\\src\\MyOfferPilot\\test_resume.pdf"
boundary = "----formdata-test-xyz"

with open(path, "rb") as f:
    file_data = f.read()

body = b""
body += ("--" + boundary + "\r\n").encode()
body += b'Content-Disposition: form-data; name="file"; filename="test_resume.pdf"\r\n'
body += b"Content-Type: application/pdf\r\n\r\n"
body += file_data + b"\r\n"
body += ("--" + boundary + "--\r\n").encode()

req = urllib.request.Request(
    "http://localhost:3003/api/parse-pdf",
    data=body,
    headers={"Content-Type": "multipart/form-data; boundary=" + boundary},
    method="POST",
)

try:
    r = urllib.request.urlopen(req, timeout=30)
    print("HTTP", r.status)
    print(r.read().decode("utf-8", errors="replace"))
except urllib.error.HTTPError as e:
    print("HTTP", e.code)
    print(e.read().decode("utf-8", errors="replace"))
except Exception as e:
    print("ERR", repr(e))