import http.cookiejar
import urllib.request
import urllib.parse
import json

cj = http.cookiejar.CookieJar()
opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(cj))

# Signup user
url = 'http://localhost:8081/signup'
data = urllib.parse.urlencode({'email': 'testsubmit@example.com', 'password': 'pass123'}).encode()
req = urllib.request.Request(url, data=data, headers={'Content-Type': 'application/x-www-form-urlencoded'})
resp = opener.open(req)
print('signup status', resp.status)
print(resp.read(200).decode('utf-8', errors='replace'))

# Submit problem 744
url = 'http://localhost:8081/api/submit'
payload = {'user_id': 1, 'problem_id': 744, 'selected_option': 1, 'time_spent': 10}
data = json.dumps(payload).encode()
req = urllib.request.Request(url, data=data, headers={'Content-Type': 'application/json'})
try:
    resp = opener.open(req)
    print('submit status', resp.status)
    print(resp.read().decode('utf-8', errors='replace'))
except urllib.error.HTTPError as e:
    print('submit status', e.code)
    print(e.read().decode('utf-8', errors='replace'))
