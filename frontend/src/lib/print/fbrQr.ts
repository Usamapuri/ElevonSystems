/**
 * The QR an FBR-filed document carries: it encodes exactly the FBR invoice
 * number, the same string the receipt prints beside it. Rendered in the
 * browser because print HTML is built client-side; this module is the one
 * place `qrcode` is imported.
 */
import QRCode from 'qrcode'

export function fbrQrDataUrl(invoiceNumber: string): Promise<string> {
  return QRCode.toDataURL(invoiceNumber, { errorCorrectionLevel: 'M', margin: 1, width: 160 })
}
