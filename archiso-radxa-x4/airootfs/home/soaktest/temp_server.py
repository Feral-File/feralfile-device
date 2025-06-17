#!/usr/bin/env python3
from http.server import BaseHTTPRequestHandler, HTTPServer
import json, datetime, os, threading, time, subprocess

CSV_FILE = "/home/soaktest/cpu_temp_log.csv"
HTML_FILE = "/home/soaktest/temp_viewer.html"
SAMPLE_INTERVAL_SECONDS = 5

def get_cpu_temp():
    try:
        output = subprocess.check_output(["sensors", "-u"], encoding="utf-8")
        lines = output.splitlines()
        in_package = False
        for line in lines:
            if "Package id 0" in line:
                in_package = True
                continue
            if in_package and "temp1_input:" in line:
                return float(line.strip().split(":")[1])
            if line.strip() == "":
                in_package = False
    except:
        return 0.0
    return 0.0

def background_logger():
    print("[INFO] Logger started")
    with open(CSV_FILE, "a") as f:
        f.write("timestamp,cpu_temp_celsius\n")
    while True:
        timestamp = datetime.datetime.now().strftime("%Y-%m-%d %H:%M:%S")
        temp = get_cpu_temp()
        with open(CSV_FILE, "a") as f:
            f.write(f"{timestamp},{temp:.1f}\n")
        time.sleep(SAMPLE_INTERVAL_SECONDS)

class TempHandler(BaseHTTPRequestHandler):
    def _send_json(self, payload):
        self.send_response(200)
        self.send_header('Content-type', 'application/json')
        self.send_header('Access-Control-Allow-Origin', '*')
        self.end_headers()
        self.wfile.write(json.dumps(payload).encode())

    def _send_html(self, path):
        try:
            with open(path, 'rb') as f:
                content = f.read()
            self.send_response(200)
            self.send_header('Content-type', 'text/html')
            self.send_header('Access-Control-Allow-Origin', '*')
            self.end_headers()
            self.wfile.write(content)
        except:
            self.send_response(404)
            self.end_headers()

    def do_GET(self):
        if self.path == '/temp':
            try:
                with open(CSV_FILE, 'r') as f:
                    last = f.readlines()[-1]
                    timestamp, temp = last.strip().split(',')
                    self._send_json({'timestamp': timestamp, 'temp': temp})
            except:
                self._send_json({'timestamp': '', 'temp': 'N/A'})
        elif self.path in ['/', '/index.html']:
            self._send_html(HTML_FILE)
        else:
            self.send_response(404)
            self.end_headers()

def run():
    threading.Thread(target=background_logger, daemon=True).start()
    HTTPServer(('', 8000), TempHandler).serve_forever()

if __name__ == '__main__':
    run()