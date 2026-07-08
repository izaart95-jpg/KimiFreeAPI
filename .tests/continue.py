import json
import struct
import sys
import asyncio
import httpx


# ===============================
# GLOBAL CHAT STATE
# ===============================
class ChatState:
    def __init__(self, chat_id: str):
        self.chat_id = chat_id
        self.last_message_id = None


chat_state = ChatState(
    chat_id="19be5aab-32f2-8b55-8000-099cd3b9dbc1"
)


# ===============================
# CONNECT ENCODER
# ===============================
def connect_encode(obj: dict) -> bytes:
    raw = json.dumps(obj, separators=(",", ":")).encode("utf-8")
    return b"\x00" + struct.pack(">I", len(raw)) + raw


# ===============================
# SEND MESSAGE (AUTO CONTINUE)
# ===============================
async def send_message(text: str, token: str) -> str | None:
    if not token:
        raise RuntimeError("No access_token")

    payload = {
        "chat_id": chat_state.chat_id,
        "scenario": "SCENARIO_K2D5",
        "tools": [{"type": "TOOL_TYPE_SEARCH", "search": {}}],
        "message": {
            "parent_id": chat_state.last_message_id,  # 🔥 AUTO
            "role": "user",
            "blocks": [
                {"message_id": "", "text": {"content": text}}
            ],
            "scenario": "SCENARIO_K2D5",
        },
        "options": {"thinking": False},
    }

    headers = {
        "accept": "*/*",
        "authorization": f"Bearer {token}",
        "connect-protocol-version": "1",
        "content-type": "application/connect+json",
        "r-timezone": "Asia/Calcutta",
        "x-language": "en-US",
        "x-msh-device-id": "7586915550627013133",
        "x-msh-platform": "web",
        "x-msh-session-id": "1731469129988841572",
        "x-traffic-id": "d4t8j3es1rh1oljov7g0",
        "referer": f"https://www.kimi.com/chat/{chat_state.chat_id}",
    }

    buffer = b""

    async with httpx.AsyncClient(timeout=None) as client:
        async with client.stream(
            "POST",
            "https://www.kimi.com/apiv2/kimi.gateway.chat.v1.ChatService/Chat",
            headers=headers,
            content=connect_encode(payload),
        ) as resp:

            async for chunk in resp.aiter_bytes():
                buffer += chunk

                while len(buffer) >= 5:
                    length = struct.unpack(">I", buffer[1:5])[0]

                    if len(buffer) < 5 + length:
                        break

                    frame = buffer[5 : 5 + length]
                    buffer = buffer[5 + length :]

                    raw = frame.decode("utf-8", errors="ignore")
                    print(raw)  # same as console.log

                    try:
                        data = json.loads(raw)
                    except json.JSONDecodeError:
                        continue

                    # ✅ AUTO UPDATE LAST MESSAGE ID
                    if data.get("message", {}).get("id"):
                        chat_state.last_message_id = data["message"]["id"]

                    # STREAM TOKENS
                    if data.get("delta", {}).get("content"):
                        sys.stdout.write(data["delta"]["content"])
                        sys.stdout.flush()

    return chat_state.last_message_id


# ===============================
# USAGE (TOP-LEVEL AWAIT EQUIV)
# ===============================
async def main():
    ACCESS_TOKEN = ""

    await send_message("Femboys Clubs in one word", ACCESS_TOKEN)
    await send_message("Tomboys Clubs", ACCESS_TOKEN)

    print("chat id:", chat_state.chat_id)
    print("last message id:", chat_state.last_message_id)


if __name__ == "__main__":
    asyncio.run(main())
