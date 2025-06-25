resp = http.post(
    url="http://frontend.hotrod.svc:8080/dispatch",
    json_body={
        "sessionID": 1,
        "requestID": 1,
        "pickupLocationID": 1,
        "dropoffLocationID": 731
    },
    capture=True,
    name="dispatchRequest"
)

notif_resp = http.get(
    url="http://frontend.hotrod.svc:8080/notifications?sessionID=1&cursor=-1",
    capture=True,
    name="fetchNotifications"
)
ck = smart_test.check("notification-api-response")
if notif_resp.status_code != 200:
    ck.error("Unexpected status code: {}", resp.status_code)
