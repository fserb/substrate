#!/usr/bin/env -S deno run --allow-net

// FastCGI protocol implementation for Deno
const FCGI_VERSION_1 = 1;
const FCGI_BEGIN_REQUEST = 1;
const FCGI_ABORT_REQUEST = 2;
const FCGI_END_REQUEST = 3;
const FCGI_PARAMS = 4;
const FCGI_STDIN = 5;
const FCGI_STDOUT = 6;
const FCGI_STDERR = 7;
const FCGI_RESPONDER = 1;

let cnt = 0;

function writeRecord(type, requestId, content) {
  const contentLength = content.length;
  const paddingLength = (8 - (contentLength % 8)) % 8;

  const header = new Uint8Array(8);
  header[0] = FCGI_VERSION_1;
  header[1] = type;
  header[2] = (requestId >> 8) & 0xff;
  header[3] = requestId & 0xff;
  header[4] = (contentLength >> 8) & 0xff;
  header[5] = contentLength & 0xff;
  header[6] = paddingLength;
  header[7] = 0;

  const padding = new Uint8Array(paddingLength);
  return new Uint8Array([...header, ...content, ...padding]);
}

function readRecord(data, offset) {
  if (data.length < offset + 8) return null;

  const version = data[offset];
  const type = data[offset + 1];
  const requestId = (data[offset + 2] << 8) | data[offset + 3];
  const contentLength = (data[offset + 4] << 8) | data[offset + 5];
  const paddingLength = data[offset + 6];

  const totalLength = 8 + contentLength + paddingLength;
  if (data.length < offset + totalLength) return null;

  const content = data.slice(offset + 8, offset + 8 + contentLength);

  return { version, type, requestId, contentLength, paddingLength, content, totalLength };
}

async function handleConnection(conn) {
  const buffer = new Uint8Array(65536);
  let offset = 0;
  let requestId = 0;

  try {
    while (true) {
      const bytesRead = await conn.read(buffer.subarray(offset));
      if (bytesRead === null) break;

      offset += bytesRead;

      let pos = 0;
      while (pos < offset) {
        const record = readRecord(buffer, pos);
        if (!record) break;

        if (record.type === FCGI_BEGIN_REQUEST) {
          requestId = record.requestId;
        } else if (record.type === FCGI_PARAMS && record.contentLength === 0) {
          // End of params, generate response
          const response = `hello ${++cnt}\n`;
          const httpResponse = `Content-Type: text/plain\r\n\r\n${response}`;

          await conn.write(writeRecord(FCGI_STDOUT, requestId, new TextEncoder().encode(httpResponse)));
          await conn.write(writeRecord(FCGI_STDOUT, requestId, new Uint8Array(0)));

          const endRequest = new Uint8Array(8);
          await conn.write(writeRecord(FCGI_END_REQUEST, requestId, endRequest));
        }

        pos += record.totalLength;
      }

      // Move remaining data to beginning of buffer
      if (pos < offset) {
        buffer.copyWithin(0, pos, offset);
      }
      offset -= pos;
    }
  } catch (error) {
    console.error("Connection error:", error);
  } finally {
    try {
      conn.close();
    } catch {}
  }
}

const listener = Deno.listen({ hostname: "127.0.0.1", port: 9000 });
console.log("FastCGI server listening on 127.0.0.1:9000");

for await (const conn of listener) {
  handleConnection(conn);
}
