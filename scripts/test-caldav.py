#!/usr/bin/env python3
"""
CalDAV/CardDAV protocol compliance test script.

Tests the DAV server against a running instance using real HTTP requests,
simulating what Apple Calendar, Thunderbird, and DAVx5 clients do.

Usage:
    pip3 install requests
    python3 scripts/test-caldav.py [--host HOST] [--port PORT] [--user USER] [--pass PASS]

Example:
    python3 scripts/test-caldav.py --host mail.fenilsonani.com --port 8443 \
        --user user@example.com --pass secret --tls
"""

import argparse
import sys
import xml.etree.ElementTree as ET

try:
    import requests
    from requests.auth import HTTPBasicAuth
except ImportError:
    print("ERROR: 'requests' package required. Install with: pip3 install requests")
    sys.exit(1)


class DAVTester:
    def __init__(self, base_url, username, password, verify_ssl=True):
        self.base_url = base_url.rstrip("/")
        self.auth = HTTPBasicAuth(username, password)
        self.username = username
        self.verify = verify_ssl
        self.passed = 0
        self.failed = 0
        self.errors = []

    def ok(self, name):
        self.passed += 1
        print(f"  PASS  {name}")

    def fail(self, name, detail=""):
        self.failed += 1
        msg = f"  FAIL  {name}"
        if detail:
            msg += f" -- {detail}"
        self.errors.append(msg)
        print(msg)

    def check(self, name, condition, detail=""):
        if condition:
            self.ok(name)
        else:
            self.fail(name, detail)

    def request(self, method, path, headers=None, data=None):
        url = self.base_url + path
        h = headers or {}
        try:
            resp = requests.request(
                method, url, auth=self.auth, headers=h,
                data=data, verify=self.verify, timeout=15,
                allow_redirects=False,
            )
            return resp
        except Exception as e:
            print(f"  ERROR  {method} {path}: {e}")
            return None

    # --- Test suites ---

    def test_well_known(self):
        print("\n== Well-Known Discovery ==")

        # CalDAV well-known
        resp = self.request("PROPFIND", "/.well-known/caldav", {"Depth": "0"})
        if resp is None:
            self.fail("well-known/caldav reachable")
            return
        self.check("well-known/caldav returns 207 or redirect",
                    resp.status_code in (207, 301, 302, 307))

        # CardDAV well-known
        resp = self.request("PROPFIND", "/.well-known/carddav", {"Depth": "0"})
        if resp is None:
            self.fail("well-known/carddav reachable")
            return
        self.check("well-known/carddav returns 207 or redirect",
                    resp.status_code in (207, 301, 302, 307))

    def test_principal(self):
        print("\n== Principal Discovery ==")

        path = f"/principals/{self.username}/"
        resp = self.request("PROPFIND", path, {"Depth": "0"})
        if resp is None:
            self.fail("principal PROPFIND reachable")
            return

        self.check("principal returns 207", resp.status_code == 207)

        body = resp.text
        self.check("principal has calendar-home-set",
                    "calendar-home-set" in body)
        self.check("principal has addressbook-home-set",
                    "addressbook-home-set" in body)
        self.check("principal has current-user-principal",
                    "current-user-principal" in body)

    def test_calendar_home(self):
        print("\n== Calendar Home PROPFIND ==")

        path = f"/calendars/{self.username}/"

        # Depth: 0
        resp = self.request("PROPFIND", path, {"Depth": "0"})
        if resp is None:
            self.fail("calendar home reachable")
            return
        self.check("calendar home Depth:0 returns 207",
                    resp.status_code == 207)
        self.check("calendar home has collection resourcetype",
                    "collection" in resp.text)

        # Depth: 1 (should include calendars)
        resp = self.request("PROPFIND", path, {"Depth": "1"})
        self.check("calendar home Depth:1 returns 207",
                    resp.status_code == 207)
        body = resp.text
        self.check("Depth:1 includes calendar resourcetype",
                    "calendar/>" in body or "calendar />" in body,
                    "No <calendar/> found in response")
        self.check("Depth:1 includes getctag",
                    "getctag" in body,
                    "No getctag found -- clients need this for sync")
        self.check("Depth:1 includes supported-calendar-component-set",
                    "supported-calendar-component-set" in body)

    def test_calendar_depth(self):
        """Test PROPFIND on a specific calendar with Depth:1 returns events."""
        print("\n== Calendar PROPFIND Depth Handling ==")

        # First, discover calendars
        path = f"/calendars/{self.username}/"
        resp = self.request("PROPFIND", path, {"Depth": "1"})
        if resp is None or resp.status_code != 207:
            self.fail("calendar discovery", "Can't list calendars")
            return

        # Parse to find a calendar UID
        cal_uid = None
        try:
            root = ET.fromstring(resp.text)
            ns = {"D": "DAV:", "C": "urn:ietf:params:xml:ns:caldav"}
            for response in root.findall("D:response", ns):
                href = response.find("D:href", ns)
                rt = response.find(".//C:calendar", ns)
                if rt is not None and href is not None:
                    parts = href.text.strip("/").split("/")
                    if len(parts) >= 3:
                        cal_uid = parts[2]
                        break
        except ET.ParseError:
            self.fail("calendar XML parseable", "Invalid XML in PROPFIND response")
            return

        if not cal_uid:
            self.fail("found at least one calendar",
                      "No calendars found. Create one first with 'mailserver user add'")
            return
        self.ok(f"found calendar: {cal_uid}")

        # Create a test event
        event_uid = "dav-test-event-001"
        event_path = f"/calendars/{self.username}/{cal_uid}/{event_uid}.ics"
        ical_data = (
            "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//Test//Test//EN\r\n"
            f"BEGIN:VEVENT\r\nUID:{event_uid}\r\nSUMMARY:DAV Test Event\r\n"
            "DTSTART:20260301T100000Z\r\nDTEND:20260301T110000Z\r\n"
            "END:VEVENT\r\nEND:VCALENDAR"
        )
        resp = self.request("PUT", event_path,
                            {"Content-Type": "text/calendar; charset=utf-8"},
                            data=ical_data)
        if resp is None:
            self.fail("PUT event")
            return
        self.check("PUT event returns 201 or 204",
                    resp.status_code in (201, 204))
        etag = resp.headers.get("ETag", "")
        self.check("PUT returns ETag", bool(etag))

        # PROPFIND Depth:1 on calendar should list the event
        cal_path = f"/calendars/{self.username}/{cal_uid}/"
        resp = self.request("PROPFIND", cal_path, {"Depth": "1"})
        self.check("calendar Depth:1 returns 207", resp.status_code == 207)
        body = resp.text
        self.check("Depth:1 includes event href",
                    f"{event_uid}.ics" in body,
                    "Event not listed in Depth:1 PROPFIND")
        self.check("Depth:1 event has getetag",
                    "getetag" in body)
        self.check("Depth:1 event has getcontenttype",
                    "getcontenttype" in body)

        # PROPFIND Depth:0 should NOT list events
        resp = self.request("PROPFIND", cal_path, {"Depth": "0"})
        self.check("Depth:0 does NOT include events",
                    f"{event_uid}.ics" not in resp.text)

        # GET event
        resp = self.request("GET", event_path)
        self.check("GET event returns 200", resp.status_code == 200)
        self.check("GET returns iCalendar data",
                    "BEGIN:VCALENDAR" in resp.text)
        self.check("GET has correct Content-Type",
                    "text/calendar" in resp.headers.get("Content-Type", ""))

        # REPORT calendar-query (no hrefs = get all)
        report_body = (
            '<?xml version="1.0" encoding="UTF-8"?>'
            '<C:calendar-query xmlns:D="DAV:" '
            'xmlns:C="urn:ietf:params:xml:ns:caldav">'
            '<D:prop><D:getetag/><C:calendar-data/></D:prop>'
            '</C:calendar-query>'
        )
        resp = self.request("REPORT", cal_path, {"Depth": "1"},
                            data=report_body)
        self.check("REPORT calendar-query returns 207",
                    resp.status_code == 207)
        self.check("REPORT includes event data",
                    "DAV Test Event" in resp.text)

        # REPORT calendar-multiget (specific hrefs)
        multiget_body = (
            '<?xml version="1.0" encoding="UTF-8"?>'
            '<C:calendar-multiget xmlns:D="DAV:" '
            'xmlns:C="urn:ietf:params:xml:ns:caldav">'
            '<D:prop><D:getetag/><C:calendar-data/></D:prop>'
            f'<D:href>{event_path}</D:href>'
            '</C:calendar-multiget>'
        )
        resp = self.request("REPORT", cal_path, {"Depth": "1"},
                            data=multiget_body)
        self.check("REPORT multiget returns 207",
                    resp.status_code == 207)
        self.check("REPORT multiget includes requested event",
                    f"{event_uid}.ics" in resp.text)

        # Cleanup: DELETE event
        resp = self.request("DELETE", event_path)
        self.check("DELETE event returns 204", resp.status_code == 204)

    def test_addressbook_home(self):
        print("\n== Address Book Home PROPFIND ==")

        path = f"/addressbooks/{self.username}/"

        resp = self.request("PROPFIND", path, {"Depth": "1"})
        if resp is None:
            self.fail("addressbook home reachable")
            return
        self.check("addressbook home Depth:1 returns 207",
                    resp.status_code == 207)
        body = resp.text
        self.check("includes addressbook resourcetype",
                    "addressbook/>" in body or "addressbook />" in body)
        self.check("includes getctag",
                    "getctag" in body)

    def test_chunked_put(self):
        """Test that PUT works without Content-Length (chunked transfer)."""
        print("\n== Chunked Transfer PUT ==")

        # First find a calendar
        path = f"/calendars/{self.username}/"
        resp = self.request("PROPFIND", path, {"Depth": "1"})
        if not resp or resp.status_code != 207:
            self.fail("chunked PUT: need a calendar first")
            return

        cal_uid = None
        try:
            root = ET.fromstring(resp.text)
            ns = {"D": "DAV:", "C": "urn:ietf:params:xml:ns:caldav"}
            for response in root.findall("D:response", ns):
                if response.find(".//C:calendar", ns) is not None:
                    href = response.find("D:href", ns)
                    parts = href.text.strip("/").split("/")
                    if len(parts) >= 3:
                        cal_uid = parts[2]
                        break
        except ET.ParseError:
            pass

        if not cal_uid:
            self.fail("chunked PUT: no calendar found")
            return

        event_path = f"/calendars/{self.username}/{cal_uid}/chunked-test.ics"
        ical_data = (
            "BEGIN:VCALENDAR\r\nVERSION:2.0\r\n"
            "BEGIN:VEVENT\r\nUID:chunked-test\r\nSUMMARY:Chunked\r\n"
            "END:VEVENT\r\nEND:VCALENDAR"
        )

        # Send with Transfer-Encoding: chunked (requests does this for generators)
        def chunked_gen():
            yield ical_data.encode()

        try:
            url = self.base_url + event_path
            resp = requests.put(
                url, auth=self.auth,
                headers={"Content-Type": "text/calendar"},
                data=chunked_gen(),
                verify=self.verify, timeout=15,
            )
            self.check("chunked PUT accepted",
                        resp.status_code in (201, 204),
                        f"Got {resp.status_code}")
        except Exception as e:
            self.fail(f"chunked PUT: {e}")

        # Cleanup
        self.request("DELETE", event_path)

    def test_options(self):
        print("\n== OPTIONS / DAV Capabilities ==")

        resp = self.request("OPTIONS", "/")
        if resp is None:
            self.fail("OPTIONS reachable")
            return
        self.check("OPTIONS returns 200", resp.status_code == 200)
        dav = resp.headers.get("DAV", "")
        self.check("DAV header includes calendar-access",
                    "calendar-access" in dav)
        self.check("DAV header includes addressbook",
                    "addressbook" in dav)

    def run_all(self):
        print(f"Testing: {self.base_url}")
        print(f"User: {self.username}")
        print("=" * 60)

        self.test_options()
        self.test_well_known()
        self.test_principal()
        self.test_calendar_home()
        self.test_calendar_depth()
        self.test_addressbook_home()
        self.test_chunked_put()

        print("\n" + "=" * 60)
        total = self.passed + self.failed
        print(f"Results: {self.passed}/{total} passed, {self.failed} failed")

        if self.errors:
            print("\nFailures:")
            for e in self.errors:
                print(e)

        return self.failed == 0


def main():
    parser = argparse.ArgumentParser(description="CalDAV/CardDAV protocol tester")
    parser.add_argument("--host", default="localhost",
                        help="Server hostname (default: localhost)")
    parser.add_argument("--port", type=int, default=8443,
                        help="Server port (default: 8443)")
    parser.add_argument("--user", default="user@example.com",
                        help="Username (email)")
    parser.add_argument("--password", "--pass", dest="password",
                        default="password", help="Password")
    parser.add_argument("--tls", action="store_true",
                        help="Use HTTPS")
    parser.add_argument("--no-verify", action="store_true",
                        help="Skip TLS certificate verification")
    args = parser.parse_args()

    scheme = "https" if args.tls else "http"
    base_url = f"{scheme}://{args.host}:{args.port}"
    verify = not args.no_verify

    tester = DAVTester(base_url, args.user, args.password, verify_ssl=verify)
    success = tester.run_all()
    sys.exit(0 if success else 1)


if __name__ == "__main__":
    main()
