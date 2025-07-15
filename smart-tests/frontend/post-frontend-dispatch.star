# Smart Test for Dispatch API

# Create a Smart Test Check
ck = smart_test.check("dispatch-api-response")

# Define the request payload
payload = {
    "sessionID": 1,
    "requestID": 1,
    "pickupLocationID": 1,
    "dropoffLocationID": 731
}

# Send the POST request with capture enabled
resp = http.post(
    url="http://frontend.hotrod.svc:8080/dispatch",
    json_body=payload,
    capture=True,
    name="dispatchRequest"
)

# Validate the response
if resp.status_code != 200:
    ck.error("Unexpected status code: {}", resp.status_code)
