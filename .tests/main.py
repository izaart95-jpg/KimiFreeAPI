import json
import struct
import httpx


# ================= CONNECT ENCODER =================
def connect_encode(obj: dict) -> bytes:
    """
    1 byte flags (0x00)
    4 bytes big-endian length
    JSON payload
    """
    raw = json.dumps(obj, separators=(",", ":")).encode("utf-8")
    header = b"\x00" + struct.pack(">I", len(raw))
    return header + raw


# ================= START NEW CHAT =================
def start_new_chat(prompt: str = "Hello"):
    token = ""

    payload = {
        "scenario": "SCENARIO_K2D5",
        "tools": [
            {"type": "TOOL_TYPE_SEARCH", "search": {}}
        ],
        "message": {
            "role": "user",
            "blocks": [
                {"message_id": "", "text": {"content": prompt}}
            ],
            "scenario": "SCENARIO_K2D5"
        },
        "options": {"thinking": False}
    }

    headers = {
        "accept": "*/*",
        "accept-language": "en-US,en;q=0.9",
        "authorization": f"Bearer {token}",
        "connect-protocol-version": "1",
        "content-type": "application/connect+json",
        "r-timezone": "Asia/Calcutta",
        "x-language": "en-US",
        "x-msh-device-id": "7586915550627013133",
        "x-msh-platform": "web",
        "x-msh-session-id": "1731469129988841572",
        "x-traffic-id": "d4t8j3es1rh1oljov7g0",
        "referer": "https://www.kimi.com/",
    }

    chat_id = None
    last_message_id = None
    buffer = b""

    with httpx.Client(timeout=None) as client:
        with client.stream(
            "POST",
            "https://www.kimi.com/apiv2/kimi.gateway.chat.v1.ChatService/Chat",
            headers=headers,
            content=connect_encode(payload),
        ) as response:

            for chunk in response.iter_bytes():
                buffer += chunk

                while len(buffer) >= 5:
                    # read frame length
                    _, length = buffer[0], struct.unpack(">I", buffer[1:5])[0]

                    if len(buffer) < 5 + length:
                        break

                    frame = buffer[5 : 5 + length]
                    buffer = buffer[5 + length :]

                    text = frame.decode("utf-8", errors="ignore")
                    print(text, flush=True)

                    try:
                        data = json.loads(text)
                    except json.JSONDecodeError:
                        continue

                    # chat id
                    if not chat_id and data.get("chat", {}).get("id"):
                        chat_id = data["chat"]["id"]
                        print("\nchat id:", chat_id)

                    if (
                        not chat_id
                        and data.get("op") == "set"
                        and data.get("chat", {}).get("id")
                    ):
                        chat_id = data["chat"]["id"]
                        print("\nchat id (wrapped):", chat_id)

                    # last message id
                    if data.get("message", {}).get("id"):
                        last_message_id = data["message"]["id"]

                    # stream tokens
                    if data.get("delta", {}).get("content"):
                        print(data["delta"]["content"], end="", flush=True)

    if not chat_id:
        print("\n⚠️ no chat id found")

    return chat_id, last_message_id


# ================= RUN =================
if __name__ == "__main__":
    chat_id, last_message_id = start_new_chat("Hello")
    print("\n\nDONE")
    print("chat id:", chat_id)
    print("last message id:", last_message_id)
