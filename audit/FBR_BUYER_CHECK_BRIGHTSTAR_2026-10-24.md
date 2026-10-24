# FBR buyer check — "Bright Star LPG Distributors", NTN J433658 (2026-10-24)

The owner asked to make sure this customer is filed under the right buyer, after Unity's six sales went to a stranger's NTN (`0723262`, audit `FBR_SANDBOX_2026-10-13.md` §2).

Customer card: NTN `J433658`, Registered, CAPITAL TERRITORY, phone 03185119002, no CNIC, no address.

## API checks (local sandbox token, seller 8951943; read-only and validate-only, nothing filed)

| Check | J433658 | 0723262 (Unity's wrong number, for comparison) |
|---|---|---|
| `POST /dist/v1/statl` (active taxpayer list, today) | **Active** | In-Active |
| `POST /dist/v1/Get_Reg_Type` | Registered | Registered |
| `validateinvoicedata_sb`, SN001, buyer province CAPITAL TERRITORY | `00 Valid` | `00 Valid` |

J433658 is a real, active, sales-tax-registered number in the shape FBR accepts, and the province is one of FBR's seven names. **None of these calls returns the owner's name**, and the comparison shows why that matters: Unity's wrong number also validated and also came back Registered. Only the ATL status differed.

## Name: not verified

FBR's DI API has no name lookup. The public verification page (`e.fbr.gov.pk/esbn/Verification`) failed its TLS handshake and points to IRIS; the IRIS link opened the owner's signed-in account (**BRIGHT STAR LPG MARKETING (PRIVATE) LIMITED**), where the session stopped without navigating.

Open for the owner, before the first filing to this customer:

1. Match J433658 to the customer's own FBR registration certificate (the Registration No printed on it, with the letter).
2. Confirm it is **not** the store's own number. The IRIS account is "Bright Star LPG Marketing (Pvt) Ltd" and the customer is "Bright Star LPG Distributors". If J433658 is the store's NTN, the card is wrong: a sale cannot be filed to the seller itself.
3. After the first filed sale, check FBR's POS Invoice Verification: it shows the buyer's name from FBR's registry (that is how the Unity mismatch was found). If the name is wrong, cancel on the DI portal at once.

## Product follow-up (not built)

ATL status would have flagged Unity's number (In-Active). A `statl` + `Get_Reg_Type` check when a Registered customer is saved, with a warning on In-Active or a type mismatch, is cheap. It still cannot prove the name.
