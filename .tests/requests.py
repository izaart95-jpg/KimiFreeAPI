#!/usr/bin/env python3
import requests
import struct
import json

ACCESS_TOKEN = "eyJhbGciOiJIUzUxMiIsInR5cCI6IkpXVCJ9.eyJpc3MiOiJ1c2VyLWNlbnRlciIsImV4cCI6MTc3NzE3Njc5OSwiaWF0IjoxNzc0NTg0Nzk5LCJqdGkiOiJkNzMwN250ZGJ2MXRmbGs1azNoMCIsInR5cCI6ImFjY2VzcyIsImFwcF9pZCI6ImtpbWkiLCJzdWIiOiJkNHQ4ajNlczFyaDFvbGpvdjdnMCIsInNwYWNlX2lkIjoiZDR0OGozNnMxcmgxb2xqb3V2N2ciLCJhYnN0cmFjdF91c2VyX2lkIjoiZDR0OGozNnMxcmgxb2xqb3V2NzAiLCJzc2lkIjoiMTczMTQ2OTEyOTk4ODg0MTU3MiIsImRldmljZV9pZCI6Ijc1ODI1MjM4Nzc4NDczODkxOTciLCJyZWdpb24iOiJvdmVyc2VhcyIsIm1lbWJlcnNoaXAiOnsibGV2ZWwiOjEwfX0.5f_Gpm_N6aG9hpwnkoW-7eiRKqURnTUzrmI-bTnYeJZBnRSfcXv3rLdMCrRoIC9uhgSi4bwy4w8x6MvWn8TG7Q"
CHAT_ID = "19db014e-a4d2-822c-8000-099c4f1338fe"

def connect_encode(obj):
    json_str = json.dumps(obj, separators=(',', ':'))
    json_bytes = json_str.encode('utf-8')
    length = len(json_bytes)
    header = b'\x00' + struct.pack('>I', length)
    return header + json_bytes

payload = {
    "chat_id": CHAT_ID,
    "scenario": "SCENARIO_K2D5_TURBO",
    "tools": [],
    "message": {
        "parent_id": "",
        "role": "user",
        "blocks": [
            {
                "message_id": "",
                "text": {"content": "Hello, how are you?"}
            }
        ],
        "scenario": "SCENARIO_K2D5_TURBO"
    },
    "options": {"thinking": False}
}

headers = {
    "accept": "*/*",
    "authorization": f"Bearer {ACCESS_TOKEN}",
    "connect-protocol-version": "1",
    "content-type": "application/connect+json",
    "r-timezone": "Asia/Calcutta",
    "x-language": "en-US",
    "x-msh-device-id": "7586915550627013133",
    "x-msh-platform": "web",
    "x-msh-session-id": "1731469129988841572",
    "x-traffic-id": "d4t8j3es1rh1oljov7g0",
    "referer": f"https://www.kimi.com/chat/{CHAT_ID}"
}

data = connect_encode(payload)
response = requests.post(
    "https://www.kimi.com/apiv2/kimi.gateway.chat.v1.ChatService/Chat",
    headers=headers,
    data=data,
    stream=True
)

print(f"Status: {response.status_code}")
for chunk in response.iter_content(chunk_size=4096):
    if chunk:
        # Parse the binary frames
        buffer = chunk
        offset = 0
        while offset + 5 <= len(buffer):
            if buffer[offset] != 0:
                offset += 1
                continue
            length = int.from_bytes(buffer[offset+1:offset+5], 'big')
            if offset + 5 + length > len(buffer):
                break
            frame = buffer[offset+5:offset+5+length]
            try:
                data = json.loads(frame.decode('utf-8'))
                # Extract text content
                if 'delta' in data and 'content' in data['delta']:
                    print(data['delta']['content'], end='', flush=True)
                elif 'block' in data and 'text' in data['block'] and 'content' in data['block']['text']:
                    print(data['block']['text']['content'], end='', flush=True)
            except:
                pass
            offset += 5 + length
print()
