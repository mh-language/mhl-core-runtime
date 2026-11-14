// `mhl graph` reports UTF-8 byte offsets; JavaScript strings (and Monaco)
// index UTF-16 code units. OffsetMap converts between the two for one
// source text. For pure-ASCII text (the common case) both are identity.

export class OffsetMap {
  private readonly ascii: boolean;
  /** byteAt[i] = UTF-8 byte offset of UTF-16 index i (length + 1 entries). */
  private readonly byteAt: Uint32Array | null = null;

  constructor(readonly text: string) {
    // eslint-disable-next-line no-control-regex
    this.ascii = /^[\x00-\x7f]*$/.test(text);
    if (this.ascii) return;
    const map = new Uint32Array(text.length + 1);
    let bytes = 0;
    for (let i = 0; i < text.length; i++) {
      map[i] = bytes;
      const c = text.charCodeAt(i);
      if (c < 0x80) bytes += 1;
      else if (c < 0x800) bytes += 2;
      else if (c >= 0xd800 && c <= 0xdbff && i + 1 < text.length) {
        // surrogate pair: 4 bytes for the whole code point. The low half is
        // not a character start; giving it the *next* character's offset
        // keeps toUtf16's "last index <= offset" search off it.
        bytes += 4;
        map[i + 1] = bytes;
        i++;
      } else bytes += 3;
    }
    map[text.length] = bytes;
    this.byteAt = map;
  }

  /** UTF-16 index of a UTF-8 byte offset (rounded down to a char start). */
  toUtf16(byteOffset: number): number {
    if (this.ascii || !this.byteAt) return Math.min(byteOffset, this.text.length);
    let lo = 0;
    let hi = this.text.length;
    while (lo < hi) {
      const mid = (lo + hi + 1) >> 1;
      if (this.byteAt[mid] <= byteOffset) lo = mid;
      else hi = mid - 1;
    }
    return lo;
  }

  toByte(utf16: number): number {
    if (this.ascii || !this.byteAt) return utf16;
    return this.byteAt[Math.max(0, Math.min(utf16, this.text.length))];
  }
}
