import base64
from openai import OpenAI
from PIL import Image
from io import BytesIO

client = OpenAI(
    api_key="sk-7VjL9xN2Q",
    base_url="https://proxyapi.haloboyi.site/",
)

response = client.images.generate(
    model="gemini-3-pro-image",
    prompt="a portrait of a sheepadoodle wearing a cape",
    response_format='b64_json',
    n=1,
)

for image_data in response.data:
  image = Image.open(BytesIO(base64.b64decode(image_data.b64_json)))
  image.show()