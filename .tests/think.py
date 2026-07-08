""" use the parent id from start chat 
""" 
import json
import struct
import sys
import httpx


# ===============================
# CONFIG (STATIC)
# ===============================
CHAT_ID = "19be5aab-32f2-8b55-8000-099cd3b9dbc1"
STATIC_PARENT_MESSAGE_ID = "19be5aab-3912-805f-8000-0a9c9fc02758"  # 🔥 always reused
ACCESS_TOKEN = ""


# ===============================
# CONNECT ENCODER
# ===============================
def connect_encode(obj: dict) -> bytes:
    raw = json.dumps(obj, separators=(",", ":")).encode("utf-8")
    return b"\x00" + struct.pack(">I", len(raw)) + raw


# ===============================
# SEND MESSAGE (STATIC PARENT)
# ===============================
def send_message(text: str):
    payload = {
        "chat_id": CHAT_ID,
        "scenario": "SCENARIO_K2D5",
        "tools": [{"type": "TOOL_TYPE_SEARCH", "search": {}}],
        "message": {
            "parent_id": STATIC_PARENT_MESSAGE_ID,  # 🔒 STATIC
            "role": "user",
            "blocks": [
                {"message_id": "", "text": {"content": text}}
            ],
            "scenario": "SCENARIO_K2D5",
        },
        "options": {"thinking": True},
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
        "referer": f"https://www.kimi.com/chat/{CHAT_ID}",
    }

    buffer = b""

    with httpx.Client(timeout=None) as client:
        with client.stream(
            "POST",
            "https://www.kimi.com/apiv2/kimi.gateway.chat.v1.ChatService/Chat",
            headers=headers,
            content=connect_encode(payload),
        ) as resp:

            for chunk in resp.iter_bytes():
                buffer += chunk

                while len(buffer) >= 5:
                    length = struct.unpack(">I", buffer[1:5])[0]

                    if len(buffer) < 5 + length:
                        break

                    frame = buffer[5 : 5 + length]
                    buffer = buffer[5 + length :]

                    raw = frame.decode("utf-8", errors="ignore")
                    print(raw)  # raw event dump

                    try:
                        data = json.loads(raw)
                    except json.JSONDecodeError:
                        continue

                    # STREAM TOKENS ONLY
                    if data.get("delta", {}).get("content"):
                        sys.stdout.write(data["delta"]["content"])
                        sys.stdout.flush()

    print("")


# ===============================
# USAGE
# ===============================
if __name__ == "__main__":
    send_message("wssp")
