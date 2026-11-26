#!/usr/bin/env python3
"""Generate F4B_DataAPI_Reference.xml (FMSaveAsXML 2.3.0.0) for FMUpgradeTool --generateDBFile.

Synthetic reference file for the File4Base Data API conformance suite (issue #208, P0).
All names and values are synthetic. Records are not part of Save-as-XML: the
"Build Reference Data" script (folder Setup) creates them from literal values
generated here, so the data set is deterministic. expected-data.json mirrors it.

Usage: python3 gen_reference.py [OUTDIR]   (default: ./build)
"""
import json, os, sys, base64
from xml.sax.saxutils import escape as _esc

FILE = "F4B_DataAPI_Reference"

def q(s):
    return _esc(str(s), {'"': "&quot;"})

def cdata(s):
    assert "]]>" not in s
    return f"<![CDATA[{s}]]>"

# --------------------------------------------------------------------------- schema
LANG = '<LanguageReference name="English" id="21"></LanguageReference>'

class F:
    """Field definition."""
    def __init__(self, name, dt="Text", kind="Normal", **kw):
        self.name, self.dt, self.kind, self.kw = name, dt, kind, kw
        self.id = None
        self.table = None

TABLES = {}   # name -> dict(id, fields(list), to)
def table(name, tid, fields):
    for i, f in enumerate(fields, 1):
        f.id = i; f.table = name
    TABLES[name] = {"id": tid, "name": name, "fields": fields,
                    "byname": {f.name: f for f in fields}}

def fld(t, n):
    return TABLES[t]["byname"][n]

table("Customers", 129, [
    F("CustomerID", "Number", serial=True, unique=True, notEmpty=True, always=True, index="All"),
    F("Code", "Text", unique=True, notEmpty=True, always=True, maxsize=8,
      comment="Unique business code, max 8 chars"),
    F("Name", "Text", notEmpty=True, always=False),
    F("Email", "Text", vcalc='IsEmpty ( Self ) or PatternCount ( Self ; "@" ) = 1',
      vmsg="Email must contain exactly one @"),
    F("Status", "Text", vlist="Statuses"),
    F("Country", "Text"),
    F("Interests", "Text"),
    F("Notes", "Text"),
    F("SecretNote", "Text", comment="NoAccess for DataAPI_ReadWrite"),
    F("Pin", "Text", comment="Text with leading zeros"),
    F("FavoriteContact", "Text"),
    F("Rating", "Number", strict="Numeric", rng=("1", "5"), always=True),
    F("Balance", "Number"),
    F("LooseNumber", "Number", comment="Free text typed into a number field"),
    F("SignupDate", "Date", strict="FourDigitYear"),
    F("PreferredTime", "Time"),
    F("Duration", "Time", comment="Durations beyond 24h and negative"),
    F("LastContact", "Timestamp"),
    F("Photo", "Binary"),
    F("Tags", "Text", reps=3),
    F("Scores", "Number", reps=4),
    F("CreatedAt", "Timestamp", ae="CreationTimestamp"),
    F("ModifiedAt", "Timestamp", ae="ModificationTimestamp"),
    F("CreatedBy", "Text", ae="CreationAccountName"),
    F("ModifiedBy", "Text", ae="ModificationAccountName"),
    F("Source", "Text", aeconst="reference-build"),
    F("cDisplayName", "Text", "Calculated", calc='Code & " – " & Name', stored=True),
    F("cBalanceRounded", "Number", "Calculated", calc="Round ( Balance ; 2 )", stored=True),
    F("cSignupPlus30", "Date", "Calculated", calc="If ( IsEmpty ( SignupDate ) ; \"\" ; SignupDate + 30 )", stored=True),
    F("cDurationPlusHour", "Time", "Calculated", calc="If ( IsEmpty ( Duration ) ; \"\" ; Duration + 3600 )", stored=True),
    F("cContactStamp", "Timestamp", "Calculated", calc="If ( IsEmpty ( SignupDate ) ; \"\" ; Timestamp ( SignupDate ; PreferredTime ) )", stored=True),
    F("cNameUpper", "Text", "Calculated", calc="Upper ( Name )", stored=False),
    F("cPhotoCopy", "Binary", "Calculated", calc="Photo", stored=False),
    F("cOrderCount", "Number", "Calculated", calc="Count ( Customer_Orders::OrderID )", stored=False),
    F("cOrdersTotal", "Number", "Calculated", calc="Sum ( Customer_Orders::Amount )", stored=False),
    F("cDivByZero", "Number", "Calculated", calc="Balance / 0", stored=False),
    F("cRepeatCalc", "Text", "Calculated", calc='Tags & "#" & Get ( CalculationRepetitionNumber )', stored=False, reps=3),
    F("cGlobalEcho", "Text", "Calculated", calc="gText", stored=False),
    F("sCount", "Number", "Summary", op="Count", of="CustomerID"),
    F("sTotalBalance", "Number", "Summary", op="Total", of="Balance"),
    F("sAvgRating", "Number", "Summary", op="Average", of="Rating"),
    F("gText", "Text", glob=True),
    F("gNumber", "Number", glob=True),
    F("gDate", "Date", glob=True),
    F("gTime", "Time", glob=True),
    F("gTimestamp", "Timestamp", glob=True),
    F("gContainer", "Binary", glob=True),
    F("gRepeat", "Text", glob=True, reps=3),
    F("gReport", "Text", glob=True, comment="Self Check output"),
])
table("Orders", 130, [
    F("OrderID", "Number", serial=True, unique=True, notEmpty=True, always=True, index="All"),
    F("CustomerID", "Number", notEmpty=True, index="All"),
    F("OrderNo", "Text", unique=True, notEmpty=True, always=True),
    F("OrderDate", "Date"),
    F("Amount", "Number"),
    F("Status", "Text"),
    F("Paid", "Number"),
    F("Comment", "Text"),
    F("FeaturedProduct", "Text"),
    F("CreatedAt", "Timestamp", ae="CreationTimestamp"),
    F("ModifiedAt", "Timestamp", ae="ModificationTimestamp"),
    F("cLineCount", "Number", "Calculated", calc="Count ( Order_Lines::LineID )", stored=False),
    F("cLineTotal", "Number", "Calculated", calc="Sum ( Order_Lines::LineTotal )", stored=False),
    F("cCustomerName", "Text", "Calculated", calc="Order_Customer::Name", stored=False),
    F("sOrderCount", "Number", "Summary", op="Count", of="OrderID"),
    F("sAmountTotal", "Number", "Summary", op="Total", of="Amount"),
])
table("OrderLines", 131, [
    F("LineID", "Number", serial=True, unique=True, notEmpty=True, always=True, index="All"),
    F("OrderID", "Number", notEmpty=True, index="All"),
    F("ProductCode", "Text", index="All"),
    F("Description", "Text"),
    F("Qty", "Number"),
    F("UnitPrice", "Number"),
    F("Discount", "Number", strict="Numeric", rng=("0", "100")),
    F("LineTotal", "Number", "Calculated", calc="Qty * UnitPrice", stored=True),
    F("CreatedAt", "Timestamp", ae="CreationTimestamp"),
    F("ModifiedAt", "Timestamp", ae="ModificationTimestamp"),
    F("sLineTotal", "Number", "Summary", op="Total", of="LineTotal"),
])
table("Contacts", 132, [
    F("ContactID", "Number", serial=True, unique=True, notEmpty=True, always=True, index="All"),
    F("CustomerID", "Number", notEmpty=True, index="All"),
    F("FirstName", "Text"),
    F("LastName", "Text", index="All"),
    F("Role", "Text"),
    F("Phone", "Text"),
    F("IsPrimary", "Number"),
    F("Birthday", "Date"),
    F("CreatedAt", "Timestamp", ae="CreationTimestamp"),
    F("ModifiedAt", "Timestamp", ae="ModificationTimestamp"),
    F("cFullName", "Text", "Calculated", calc='FirstName & " " & LastName', stored=True),
])
table("Products", 133, [
    F("ProductID", "Number", serial=True, unique=True, notEmpty=True, always=True, index="All"),
    F("ProductCode", "Text", unique=True, notEmpty=True, always=True, index="All"),
    F("ProductName", "Text"),
    F("Price", "Number"),
    F("Active", "Number"),
])

# Table occurrences: name -> base table
TOS = [
    ("Customers", "Customers"), ("Customer_Orders", "Orders"), ("Customer_Order_Lines", "OrderLines"),
    ("Customer_Contacts", "Contacts"), ("Customers_SameCountry", "Customers"),
    ("Orders", "Orders"), ("Order_Lines", "OrderLines"), ("Order_Customer", "Customers"),
    ("OrderLines", "OrderLines"), ("Line_Order", "Orders"), ("Line_Order_Customer", "Customers"),
    ("Line_Product", "Products"),
    ("Contacts", "Contacts"), ("Contact_Customer", "Customers"),
    ("Products", "Products"),
]
TO = {}
for i, (n, bt) in enumerate(TOS):
    TO[n] = {"id": 1065089 + i, "name": n, "bt": bt}

def toref(n, tag="TableOccurrenceReference"):
    return f'<{tag} id="{TO[n]["id"]}" name="{q(n)}"></{tag}>'

def btref(t):
    return f'<BaseTableReference id="{TABLES[t]["id"]}" name="{q(t)}"></BaseTableReference>'

def fref_to(to, fname, rep=None, inner=""):
    f = fld(TO[to]["bt"], fname)
    r = f' repetition="{rep}"' if rep is not None else ""
    return f'<FieldReference id="{f.id}" name="{q(fname)}"{r}>{inner}{toref(to)}</FieldReference>'

def fref_bt(t, fname):
    f = fld(t, fname)
    return f'<FieldReference id="{f.id}" name="{q(fname)}">{btref(t)}</FieldReference>'

# Relationships: (left TO, left field, right TO, right field, opts)
RELS = [
    ("Customers", "CustomerID", "Customer_Orders", "CustomerID", {"rcreate": True}),
    ("Customer_Orders", "OrderID", "Customer_Order_Lines", "OrderID", {}),
    ("Customers", "CustomerID", "Customer_Contacts", "CustomerID", {"rcreate": True, "rsort": [("LastName", "Ascending"), ("FirstName", "Ascending")]}),
    ("Customers", "Country", "Customers_SameCountry", "Country", {}),
    ("Orders", "OrderID", "Order_Lines", "OrderID", {"rcreate": True, "rdelete": True}),
    ("Orders", "CustomerID", "Order_Customer", "CustomerID", {}),
    ("OrderLines", "OrderID", "Line_Order", "OrderID", {}),
    ("Line_Order", "CustomerID", "Line_Order_Customer", "CustomerID", {}),
    ("OrderLines", "ProductCode", "Line_Product", "ProductCode", {}),
    ("Contacts", "CustomerID", "Contact_Customer", "CustomerID", {}),
]

# Value lists
VLISTS = [
    ("Countries", "custom", ["France", "Deutschland", "España", "Österreich", "Côte d'Ivoire",
                             "日本", "Ελλάδα", "Россия", "United States", "Brasil"]),
    ("Statuses", "custom", ["Active", "Inactive", "Prospect", "Archivé"]),
    ("Interests", "custom", ["Hardware", "Software", "Services", "Formation", "Maintenance"]),
    ("OrderStatuses", "custom", ["Draft", "Confirmed", "Shipped", "Cancelled"]),
    ("ContactRoles", "custom", ["Billing", "Technical", "Sales", "Management"]),
    ("Ratings", "custom", ["1", "2", "3", "4", "5"]),
    ("ProductCodes", "field", dict(to="Products", primary="ProductCode", secondary="ProductName",
                                   pshow=True, sshow=False, related=None)),
    ("ProductNames_ByCode", "field", dict(to="Products", primary="ProductCode", secondary="ProductName",
                                          pshow=False, sshow=True, related=None)),
    ("CustomerContactNames", "field", dict(to="Customer_Contacts", primary="cFullName", secondary=None,
                                           pshow=True, sshow=False, related="Customers")),
]
VL = {n: i for i, (n, _, _) in enumerate(VLISTS, 1)}

def vlref(n):
    return f'<ValueListReference id="{VL[n]}" name="{q(n)}"></ValueListReference>'

# --------------------------------------------------------------------------- data
PNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="
GIF = "R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7"
TXT = base64.b64encode("F4B reference container text file\n".encode()).decode()

CUST_NAMES = [
    "Alpha Industries", "Zoë Ångström & Fils", "Łódź Żółć Sp. z o.o.", "Ελληνική Εταιρεία",
    "ООО «Ромашка»", "株式会社テスト", "شركة الاختبار", "חברת בדיקה", "Nguyễn Văn Trading",
    "Emoji 😀 Shop", "O'Brien \"Quoted\" Ltd", "  Leading and trailing spaces  ",
    "Café Crème SARL", "<b>Markup</b> & Co", "Tab|TAB|Inside", "Multi|NL|Line Name", "A",
    "Long " * 40 + "End", "Straße Müller GmbH", "Ñandú Español S.A.", "Çağrı Türk A.Ş.",
    "Δέλτα Delta Mixed", "数字 123 Numbers", "Backslash \\ Path", "Percent % and Underscore _",
    "Semicolon; Comma, Colon:", "Ümlaut Ötzi", "Ørsted Nordic ApS", "Šoštanj d.o.o.", "Zulu Last",
]
BALANCES = [
    "0", "1", "-1", "3.14159265358979323846264338327950288419716939937510",
    "-0.000000000000000000123", "123456789012345678901234567890", "999999999999999999",
    "1000000000000000000", "0.1", "0.2", "-273.15", "", "12.50", "007", "1.0", "-0",
    "0.30000000000000004", "9007199254740993", "-9007199254740993", "1234.5678", "42", "",
    "100", "2.5", "-99999.99", "0.000001", "65535", "3", "-123456789012345678.123456789", "1e3",
]
LOOSE = [
    "abc", "12abc", "1,5", " 7 ", "1e10", "1E-5", "∞", "--5", "5-", "$1,234.56", "50%", "0x1F",
    "١٢٣", "", "NaN", "true", "3 4", ".5", "5.", "+5", "١٫٥", "½", "1 000 000", "(12)", "12:30",
    "2024-01-01", "-", "e", "0000", "",
]
DATES = [
    "0001-01-01", "1066-10-14", "1582-10-15", "1899-12-30", "1899-12-31", "1900-01-01",
    "1900-02-28", "1904-01-01", "1969-12-31", "1970-01-01", "", "2000-02-29", "2001-09-09",
    "2020-02-29", "2024-12-31", "2026-11-26", "2038-01-19", "2038-01-20", "2099-12-31",
    "2100-03-01", "", "2999-06-15", "3000-01-01", "3999-12-31", "4000-12-31", "1999-12-31",
    "1789-07-14", "1515-09-13", "0999-05-05", "1800-01-01",
]
TIMES = [0, 1, 43200, 86399, 3661, None, 45296.5, 30600, 64800, 120, 59.999, 7200, 50000, 1,
         86399.999, None, 36000, 28800, 61200, 15, 3599, 3600, 43199, 43201, 900, 1800, 2700,
         82800, 72000, 33333]
DURATIONS = [90000, 360000, -5400, 0, 86400, 3600.25, 3600000000, None, -1, 172800, 1, 59,
             -86400, 99 * 3600 + 59 * 60 + 59, 24 * 3600, 25 * 3600 + 0.5, None, 7 * 86400,
             -360000, 12345, 3 * 3600, 100, 1000, 10000, 100000, 1000000, 10000000, 2, -2, 0.001]
def ts(i):
    if i % 9 == 0:
        return None
    d = DATES[(i * 7) % len(DATES)] or "2024-06-15"
    secs = [0, 1, 45296, 86399, 45296.123, 3600, 12345.5, 75600][i % 8]
    return (d, secs)

INTERESTS = ["Hardware", "Software", "Services", "Formation", "Maintenance"]
STATUSES = ["Active", "Inactive", "Prospect", "Archivé"]
COUNTRIES = [v for n, k, v in VLISTS if n == "Countries"][0]

def customers():
    out = []
    for i in range(1, 31):
        n_ord = i % 5
        r = {
            "CustomerID": str(i), "Code": f"C{i:03d}", "Name": CUST_NAMES[i - 1],
            "Email": "" if i % 6 == 0 else ("UPPER@EXAMPLE.TEST" if i == 7 else
                      ("first+tag@example.test" if i == 8 else f"contact{i}@example.test")),
            "Status": "" if i % 7 == 0 else STATUSES[i % 4],
            "Country": "" if i % 11 == 0 else COUNTRIES[i % len(COUNTRIES)],
            "Interests": "|NL|".join(INTERESTS[j] for j in range(5) if (i >> j) & 1),
            "Notes": "" if i % 4 == 0 else (
                ("Long note " + "lorem ipsum dolor sit amet " * 80).strip() if i == 5 else
                f"Note for customer {i}|NL|second line é à ü|NL|third line"),
            "SecretNote": f"secret-{i:02d}",
            "Pin": f"{(i * 37) % 10000:04d}",
            "Rating": "" if i % 6 == 0 else str((i % 5) + 1),
            "Balance": BALANCES[i - 1], "LooseNumber": LOOSE[i - 1],
            "SignupDate": DATES[i - 1],
            "PreferredTime": TIMES[i - 1], "Duration": DURATIONS[i - 1], "LastContact": ts(i),
            "Photo": {1: ("png", PNG, "pixel.png"), 2: ("gif", GIF, "pixel.gif"),
                      3: ("txt", TXT, "note.txt")}.get(i),
            "Tags": [f"tag{i}a", "" if i % 2 else f"tag{i}b", "ünïcødé" if i % 3 == 0 else ""],
            "Scores": [str(i), "" if i % 4 == 0 else str(i * -1.5), "0.125" if i % 2 else "",
                       "1e2" if i == 1 else str(i * 1000)],
            "_orders": n_ord,
        }
        out.append(r)
    return out

def contacts(cust):
    out = []; cid = 0
    first = ["Jean", "Zoë", "Łukasz", "Ελένη", "Дмитрий", "太郎", "محمد", "שרה", "Ngọc", "José"]
    last = ["Martin", "Ångström", "Wójcik", "Παπαδοπούλου", "Иванов", "山田", "علي", "כהן", "Trần", "Muñoz"]
    roles = ["Billing", "Technical", "Sales", "Management"]
    for c in cust:
        i = int(c["CustomerID"])
        for k in range(i % 3):
            cid += 1
            out.append({"ContactID": str(cid), "CustomerID": str(i),
                        "FirstName": first[cid % 10], "LastName": last[(cid * 3) % 10],
                        "Role": roles[cid % 4], "Phone": f"+33 1 {cid:02d} 00 00 {i:02d}" if cid % 5 else "",
                        "IsPrimary": "1" if k == 0 else "0",
                        "Birthday": DATES[(cid * 5) % 30] or "1980-05-17"})
    for c in cust:
        mine = [x for x in out if x["CustomerID"] == c["CustomerID"]]
        c["FavoriteContact"] = (mine[-1]["FirstName"] + " " + mine[-1]["LastName"]) if mine else ""
    return out

def orders(cust):
    out = []; oid = 0
    amounts = ["1234.56", "-50.00", "0", "99999999.99", "0.005", "19.99", "100", "", "3.333333333333333333", "250.5"]
    ostat = ["Draft", "Confirmed", "Shipped", "Cancelled"]
    for c in cust:
        for k in range(c["_orders"]):
            oid += 1
            if oid == 1: d = "1999-12-31"
            elif oid == 2: d = "2100-01-01"
            elif oid % 13 == 0: d = ""
            else:
                import datetime
                d = (datetime.date(2020, 1, 1) + datetime.timedelta(days=oid * 17)).isoformat()
            out.append({"OrderID": str(oid), "CustomerID": c["CustomerID"], "OrderNo": f"SO-{oid:04d}",
                        "OrderDate": d, "Amount": amounts[oid % 10], "Status": ostat[oid % 4],
                        "Paid": ["0", "1", ""][oid % 3],
                        "Comment": "" if oid % 2 else f"Commande n°{oid} – livrée à l'entrepôt|NL|ligne 2",
                        "FeaturedProduct": f"P{(oid % 30) + 1:03d}"})
    return out

def lines(ords):
    out = []; lid = 0
    qtys = ["1", "2", "10", "2.5", "-1", "0", "1000000", "3"]
    prices = ["9.99", "0.01", "1234.5", "19.999", "0", "-5", "100", "33.333333"]
    for o in ords:
        oid = int(o["OrderID"])
        for k in range(1 + oid % 3):
            lid += 1
            out.append({"LineID": str(lid), "OrderID": o["OrderID"],
                        "ProductCode": f"P{((lid * 7) % 30) + 1:03d}",
                        "Description": f"Ligne {lid} — article spécial" if lid % 4 else "",
                        "Qty": qtys[lid % 8], "UnitPrice": prices[(lid * 3) % 8],
                        "Discount": str([0, 5, 10, 12.5, 100, 0, 50, 33][lid % 8])})
    return out

def products():
    names = ["Écran 27\"", "Clavier AZERTY", "Souris sans fil", "Câble HDMI 2 m", "Imprimante laser",
             "Disque SSD 1 To", "Mémoire 16 Go", "Onduleur 1500 VA", "Switch 24 ports", "Routeur Wi‑Fi",
             "Lecteur code-barres", "Tablette 10\"", "Station d'accueil", "Webcam HD", "Casque audio",
             "Prise réseau", "Baie 42U", "Ventilateur", "Lampe LED", "Batterie externe",
             "Pâte thermique", "Adaptateur USB‑C", "Hub USB", "Carte graphique", "Alimentation 650 W",
             "Boîtier PC", "Écran tactile", "Scanner A4", "Projecteur", "Produit sans nom"]
    prices = ["199.99", "29.9", "19.5", "4.99", "349", "89.99", "59.95", "420", "129", "79",
              "149.5", "299", "189", "49.99", "39", "2.5", "899", "12.99", "8.5", "25",
              "6.75", "14.9", "17", "650", "75", "65", "399", "120", "540", ""]
    return [{"ProductID": str(i), "ProductCode": f"P{i:03d}", "ProductName": names[i - 1],
             "Price": prices[i - 1], "Active": "0" if i % 6 == 0 else "1"} for i in range(1, 31)]

CUST = customers(); CONT = contacts(CUST); ORD = orders(CUST); LIN = lines(ORD); PROD = products()

# --------------------------------------------------------------------------- FM calc helpers
def fmstr(s):
    return '"' + s.replace("\\", "\\\\").replace('"', '\\"') + '"'

def fmlist(values):
    for v in values:
        assert "¶" not in v and "\n" not in v and "\r" not in v, v
    return fmstr("¶".join(values))

def num(x):
    if x is None: return ""
    s = repr(x)
    return s[:-2] if s.endswith(".0") else s

# --------------------------------------------------------------------------- script steps
class S:
    def __init__(self):
        self.steps = []
    def add(self, sid, name, opts, params=""):
        self.steps.append((sid, name, opts, params))
        return self

def P(t, body):
    return f'<Parameter type="{t}">{body}</Parameter>'

def CALC(text, dt=1, pos=0):
    return f'<Calculation datatype="{dt}" position="{pos}"><Calculation><Text>{cdata(text)}</Text></Calculation></Calculation>'

def st_comment(s, txt): return s.add(89, "# (comment)", 4, P("Comment", f'<Comment value="{q(txt)}"></Comment>'))
def st_errcap(s, on=True): return s.add(86, "Set Error Capture", 196608 if on else 65536, P("Boolean", f'<Boolean id="131072" value="{on}"></Boolean>'))
def st_goto_layout(s, lname):
    lid = LAYOUT_ID[lname]
    return s.add(6, "Go to Layout", 10, P("LayoutReferenceContainer", f'<LayoutReferenceContainer value="5"><LayoutReference id="{lid}" name="{q(lname)}"></LayoutReference></LayoutReferenceContainer>') + P("Animation", '<Animation name="None" value="0"></Animation>'))
def st_new(s): return s.add(7, "New Record/Request", 0)
def st_setfield(s, to, fname, text, rep=None):
    inner = ""
    if rep is not None:
        inner = f'<repetition>{CALC(str(rep), 1, 10)}</repetition>'
    return s.add(76, "Set Field", 16385, P("FieldReference", fref_to(to, fname, None, inner)) + P("Calculation", CALC(text)))
def st_setvar(s, name, text, rep="1"):
    return s.add(141, "Set Variable", 16388, P("Variable", f'<value>{CALC(text, 1, 1)}</value><Name value="{q(name)}"></Name><repetition>{CALC(rep, 1, 2)}</repetition>'))
def st_loop(s): return s.add(71, "Loop", 0, P("Boolean", '<Boolean type="Collapsed" id="33554432" value="False"></Boolean>') + P("List", '<List name="Always" value="1"></List>'))
def st_exitloopif(s, text): return s.add(72, "Exit Loop If", 16384, P("Calculation", CALC(text, 7)))
def st_endloop(s): return s.add(73, "End Loop", 0)
def st_commit(s, skip=False):
    o = 128 + (256 if skip else 0)
    return s.add(75, "Commit Records/Requests", o, P("Boolean", f'<Boolean type="Skip data entry validation" id="256" value="{skip}"></Boolean>') + P("Boolean", '<Boolean type="With dialog" id="128" value="False"></Boolean>') + P("Boolean", '<Boolean type="Force Commit" id="512" value="False"></Boolean>'))
def st_exit(s, text=None):
    if text is None: return s.add(103, "Exit Script", 0)
    return s.add(103, "Exit Script", 16384, P("Calculation", CALC(text)))
def st_findmode(s): return s.add(22, "Enter Find Mode", 0, P("Boolean", '<Boolean type="Pause" id="16777216" value="False"></Boolean>'))
def st_find(s): return s.add(28, "Perform Find", 0)
def st_sort(s, to, specs):
    sl = "".join(f'<Sort type="{d}"><PrimaryField>{fref_to(to, f)}</PrimaryField></Sort>' for f, d in specs)
    return s.add(39, "Sort Records", 33587328, P("Boolean", '<Boolean type="With dialog" id="128" value="False"></Boolean>') + P("Restore", '<Restore value="True"></Restore>') + P("SortSpecification", f'<SortSpecification value="True" blanksLast="False" maintain="True"><SortList membercount="{len(specs)}">{sl}</SortList></SortSpecification>'))
def st_omit(s): return s.add(25, "Omit Record", 0)
def st_showall(s): return s.add(23, "Show All Records", 0)
def st_delete_all(s): return s.add(10, "Delete All Records", 128, P("Boolean", '<Boolean type="With dialog" id="128" value="False"></Boolean>'))
def st_if(s, text): return s.add(68, "If", 16384, P("Boolean", '<Boolean type="Collapsed" id="33554432" value="False"></Boolean>') + P("Calculation", CALC(text, 7)))
def st_endif(s): return s.add(70, "End If", 0)
def st_gotorec(s, which):
    v = {"First": 1, "Last": 2, "Previous": 3, "Next": 4}[which]
    eal = '<Boolean type="Exit after last" position="187" value="False"></Boolean>' if which in ("Next", "Previous") else ""
    return s.add(16, "Go to Record/Request/Page", 2, P("Records", f'<List name="{which}" value="{v}">{eal}</List>'))
def st_textruler(s): return s.add(92, "Show/Hide Text Ruler", 2, P("List", '<List name="Hide" value="2"></List>'))
def st_perform(s, sname):
    sid = SCRIPT_ID[sname]
    return s.add(1, "Perform Script", 66, P("List", f'<List name="From list" value="1"><ScriptReference id="{sid}" name="{q(sname)}"></ScriptReference></List>') + P("Parameter", "<Parameter></Parameter>"))

# --------------------------------------------------------------------------- layouts
# Layout catalog entries: ("layout", name, to, objects) | ("folder", name) | ("end", name) | ("sep",)
def E(to, fname, vl=None, style=0, show=1, rep=1):
    return ("field", to, fname, vl, style, show, rep)
def PORTAL(name, to, fields, sort=None, filt=None, rows=5):
    return ("portal", name, to, fields, sort, filt, rows)

C = "Customers"
def all_fields(to):
    out = []
    for f in TABLES[TO[to]["bt"]]["fields"]:
        if f.name == "gReport": continue
        out.append(E(to, f.name, show=f.kw.get("reps", 1)))
    return out

LAYOUTS = [
    ("layout", "Customers", "Customers", all_fields("Customers")),
    ("layout", "Orders", "Orders", all_fields("Orders")),
    ("layout", "OrderLines", "OrderLines", all_fields("OrderLines")),
    ("layout", "Contacts", "Contacts", all_fields("Contacts")),
    ("layout", "Products", "Products", all_fields("Products")),
    ("layout", "Setup_Report", "Customers", [("big", E(C, "gReport"))]),
    ("sep", "-", "Customers"),
    # FMUpgradeTool --generateDBFile turns layout folder end markers (isFolder="Marker") into
    # plain layouts, so folders are closed implicitly by the end of the list: the API folder
    # is last and its nested folder is the last item inside it.
    ("folder", "API"),
    ("layout", "API_Customers", "Customers", [
        E(C, "CustomerID"), E(C, "Code"), E(C, "Name"), E(C, "Name"), E(C, "Email"),
        E(C, "Status", "Statuses", 4), E(C, "Country", "Countries", 2), E(C, "Interests", "Interests", 3),
        E(C, "Pin"), E(C, "Rating", "Ratings", 1), E(C, "Balance"), E(C, "LooseNumber"),
        E(C, "SignupDate", None, 6), E(C, "PreferredTime"), E(C, "Duration"), E(C, "LastContact"),
        E(C, "Photo"), E(C, "Tags", show=3), E(C, "Scores", show=2),
        E(C, "CreatedAt"), E(C, "ModifiedAt"), E(C, "CreatedBy"), E(C, "ModifiedBy"), E(C, "Source"),
        E(C, "cDisplayName"), E(C, "cNameUpper"), E(C, "cBalanceRounded"), E(C, "cSignupPlus30"),
        E(C, "cDurationPlusHour"), E(C, "cContactStamp"), E(C, "cPhotoCopy"), E(C, "cOrderCount"),
        E(C, "cOrdersTotal"), E(C, "cDivByZero"), E(C, "cRepeatCalc", show=3), E(C, "cGlobalEcho"),
        E(C, "sCount"), E(C, "sTotalBalance"), E(C, "sAvgRating"),
        E(C, "gText"), E(C, "gNumber"), E(C, "gDate"), E(C, "gTime"), E(C, "gTimestamp"),
        E(C, "gContainer"), E(C, "gRepeat", show=3),
        E("Customer_Contacts", "FirstName"), E("Customer_Orders", "OrderNo"),
    ]),
    ("layout", "API_Customers_Portals", "Customers", [
        E(C, "CustomerID"), E(C, "Code"), E(C, "Name"), E(C, "Country"),
        E(C, "FavoriteContact", "CustomerContactNames", 2),
        ("offlayout", E(C, "Notes")),
        PORTAL("OrdersPortal", "Customer_Orders",
               ["OrderID", "OrderNo", "OrderDate", "Amount", "Status", "Paid"],
               sort=[("OrderDate", "Descending")]),
        PORTAL("PaidOrdersPortal", "Customer_Orders", ["OrderID", "OrderNo", "Amount"],
               filt="Customer_Orders::Paid = 1"),
        PORTAL("", "Customer_Contacts", ["ContactID", "FirstName", "LastName", "Role"],
               filt='Customer_Contacts::Role ≠ "Billing"'),
        PORTAL("OrderLinesPortal", "Customer_Order_Lines", ["LineID", "OrderID", "ProductCode", "Qty", "LineTotal"],
               sort=[("OrderID", "Ascending"), ("LineID", "Descending")], rows=8),
        PORTAL("SameCountryPortal", "Customers_SameCountry", ["CustomerID", "Name", "Country"],
               filt="Customers_SameCountry::CustomerID ≠ Customers::CustomerID",
               sort=[("CustomerID", "Ascending")]),
    ]),
    ("layout", "API_Customers_Restricted", "Customers", [
        E(C, "CustomerID"), E(C, "Code"), E(C, "Name"), E(C, "SecretNote"), E(C, "Rating")]),
    ("layout", "API_Orders", "Orders", [
        E("Orders", f) for f in ["OrderID", "CustomerID", "OrderNo"]] + [
        E("Orders", "OrderDate", None, 6), E("Orders", "Amount"), E("Orders", "Status", "OrderStatuses", 2),
        E("Orders", "Paid"), E("Orders", "Comment"), E("Orders", "FeaturedProduct", "ProductNames_ByCode", 2),
        E("Orders", "CreatedAt"), E("Orders", "ModifiedAt"), E("Orders", "cLineCount"),
        E("Orders", "cLineTotal"), E("Orders", "cCustomerName"), E("Orders", "sOrderCount"),
        E("Orders", "sAmountTotal"), E("Order_Customer", "Name"), E("Order_Customer", "Code"),
        PORTAL("LinesPortal", "Order_Lines", ["LineID", "ProductCode", "Description", "Qty", "UnitPrice", "LineTotal"],
               sort=[("ProductCode", "Ascending")]),
    ]),
    ("layout", "API_OrderLines", "OrderLines", [
        E("OrderLines", "LineID"), E("OrderLines", "OrderID"), E("OrderLines", "ProductCode", "ProductCodes", 1),
        E("OrderLines", "Description"), E("OrderLines", "Qty"), E("OrderLines", "UnitPrice"),
        E("OrderLines", "Discount"), E("OrderLines", "LineTotal"), E("OrderLines", "sLineTotal"),
        E("Line_Order", "OrderNo"), E("Line_Order", "OrderDate"), E("Line_Order_Customer", "Name"),
        E("Line_Product", "ProductName")]),
    ("layout", "API_Contacts", "Contacts", [
        E("Contacts", "ContactID"), E("Contacts", "CustomerID"), E("Contacts", "FirstName"),
        E("Contacts", "LastName"), E("Contacts", "Role", "ContactRoles", 4), E("Contacts", "Phone"),
        E("Contacts", "IsPrimary"), E("Contacts", "Birthday", None, 6), E("Contacts", "cFullName"),
        E("Contact_Customer", "Name")]),
    ("layout", "API_Products", "Products", [E("Products", f) for f in ["ProductID", "ProductCode", "ProductName", "Price", "Active"]]),
    ("layout", "API_NoAccess", "Products", [E("Products", "ProductID"), E("Products", "ProductCode")]),
    ("layout", "API_Empty", "Customers", []),
    ("layout", "API_Duplicate", "Customers", [E(C, "CustomerID"), E(C, "Name")]),
    ("layout", "API_Duplicate", "Orders", [E("Orders", "OrderID"), E("Orders", "OrderNo")]),
    ("sep", "-", "Customers"),
    ("folder", "Nested"),
    ("layout", "API_Customers_Nested", "Customers", [E(C, "CustomerID"), E(C, "Code")]),
]
LAYOUT_ID = {}
for i, L in enumerate(LAYOUTS, 1):
    if L[0] == "layout" and L[1] not in LAYOUT_ID:
        LAYOUT_ID[L[1]] = i

def field_obj(oid, e, top, left, width=260, height=20):
    _, to, fname, vl, style, show, rep = e
    tname = {0: "Edit Box", 1: "Drop-down List", 2: "Pop-up Menu", 3: "Checkbox Set",
             4: "Radio Button Set", 6: "Drop-down Calendar"}[style]
    f = fld(TO[to]["bt"], fname)
    if f.dt == "Binary": tname = "Container"; height = 60
    nreps = abs(show)
    h = height * nreps if style == 0 and f.dt != "Binary" else height
    if style in (3, 4): h = 60
    disp = f'<Display Style="{style}" show="{show}">{vlref(vl) if vl else ""}</Display>'
    return (f'<LayoutObject id="{oid}" type="{tname}" name="" kind="1">'
            f'<Bounds top="{top}" left="{left}" bottom="{top + h}" right="{left + width}"></Bounds>'
            f'<Options>805306368</Options><Field>{fref_to(to, fname, rep)}<Options>32</Options>{disp}'
            f'<Usage inputMode="0" type="0"></Usage></Field></LayoutObject>'), h

def portal_obj(oid, p, top, left):
    _, name, to, fields, sort, filt, rows = p
    rowh = 20; colw = 120
    flags = 16 + (8 if sort else 0) + (128 if filt else 0)
    inner = []; oid0 = oid
    for k, fn in enumerate(fields):
        oid += 1
        x, _ = field_obj(oid, E(to, fn), top, left + k * colw, colw - 4, rowh)
        inner.append(x)
    sortx = ""
    if sort:
        sl = "".join(f'<Sort type="{d}"><PrimaryField>{fref_to(to, f)}</PrimaryField></Sort>' for f, d in sort)
        sortx = f'<SortSpecification value="True" blanksLast="False" maintain="True"><SortList membercount="{len(sort)}">{sl}</SortList></SortSpecification>'
    filtx = f'<Calculation><Text>{cdata(filt)}</Text></Calculation>' if filt else ""
    h = rowh * rows
    xml = (f'<LayoutObject id="{oid0}" type="Portal" name="{q(name)}" kind="9">'
           f'<Bounds top="{top}" left="{left}" bottom="{top + h}" right="{left + colw * len(fields)}"></Bounds>'
           f'<Options>805306368</Options><Portal>{toref(to)}<Options index="1" show="{rows}">{flags}</Options>'
           f'{sortx}{filtx}<ObjectList membercount="{len(fields)}">{"".join(inner)}</ObjectList></Portal></LayoutObject>')
    return xml, h, oid

def layout_xml(i, L):
    kind = L[0]
    common_tail = ('<GridStyle><Color red="0.500000" green="0.500000" blue="0.500000" alpha="1.000000"></Color><Style>5</Style></GridStyle>'
                   '<ClientType>0</ClientType>')
    if kind == "folder":
        return f'<Layout id="{i}" name="{q(L[1])}" isFolder="True"><TagList></TagList>{common_tail}<Options hidden="False">0</Options></Layout>'
    if kind == "end":
        return f'<Layout id="{i}" name="{q(L[1])}" isFolder="Marker"><TagList></TagList>{common_tail}<Options hidden="False">0</Options></Layout>'
    to = L[2]
    if kind == "sep":
        return (f'<Layout id="{i}" name="-" width="1000" isSeparatorItem="True">{toref(to)}<LayoutThemeReference></LayoutThemeReference>'
                f'<PartsList membercount="1"><Part type="Body" kind="4"><Definition type="Body" kind="4" size="100" absolute="0" Options="0"></Definition></Part></PartsList>'
                f'<TagList></TagList>{common_tail}<Options hidden="False">0</Options></Layout>')
    objs = []; oid = 0; top = 10; col = 0; colTop = 10; maxbottom = 100
    x0 = [20, 340, 660]
    for e in L[3]:
        if e[0] == "field":
            oid += 1
            xml, h = field_obj(oid, e, top, x0[col])
            objs.append(xml); top += h + 6
            if top > 1100 and col < 2: col += 1; top = colTop
        elif e[0] == "offlayout":
            oid += 1
            xml, h = field_obj(oid, e[1], 20, 1100)
            objs.append(xml)
        elif e[0] == "big":
            oid += 1
            xml, h = field_obj(oid, e[1], 10, 20, 940, 600)
            objs.append(xml); top += 610
        elif e[0] == "portal":
            if col == 0 and top < 300: pass
            oid += 1
            xml, h, oid = portal_obj(oid, e, top, 20)
            objs.append(xml); top += h + 12
        maxbottom = max(maxbottom, top)
    body = max(maxbottom + 20, 400 if objs else 100)
    olist = f'<ObjectList membercount="{len(objs)}">{"".join(objs)}</ObjectList>' if objs else ""
    return (f'<Layout id="{i}" name="{q(L[1])}" width="1000">{toref(to)}<LayoutThemeReference></LayoutThemeReference>'
            f'<PartsList membercount="1"><Part type="Body" kind="4"><Definition type="Body" kind="4" size="{body}" absolute="0" Options="0"></Definition>'
            f'{olist}</Part></PartsList><TagList></TagList>{common_tail}<Options hidden="False">0</Options></Layout>')

# --------------------------------------------------------------------------- scripts
SCRIPTS = [  # (kind, name)
    ("folder", "API"),
    ("script", "API_Success"),
    ("script", "API_EchoParam"),
    ("script", "API_SecondParamValue"),
    ("script", "API_Error_NoRecords"),
    ("script", "API_Error_ExitResult"),
    ("script", "API_Unsupported"),
    ("script", "API_FindActive"),
    ("script", "API_OmitFirst"),
    ("script", "API_SortByNameDesc"),
    ("script", "API_SetGlobal"),
    ("script", "API_SetVar"),
    ("script", "API_ReadVar"),
    ("script", "API_UnicodeResult"),
    ("sep", "-"),
    ("folder", "Diagnostics"),
    ("script", "API_ReturnContext"),
    ("end", "Diagnostics"),
    ("end", "API"),
    ("folder", "Setup"),
    ("script", "Build Reference Data"),
    ("script", "Self Check"),
    ("end", "Setup"),
] + ([("script", n) for n in ("DBG_IfFalse", "DBG_IfTrue", "DBG_Loop")] if os.environ.get("F4B_DEBUG") else [])
SCRIPT_ID = {n: i for i, (k, n) in enumerate(SCRIPTS, 1) if k == "script"}

def lit_text(v):
    """FileMaker calc literal for a data value; |NL| -> carriage return, |TAB| -> tab."""
    out = []
    for i, line in enumerate(v.split("|NL|")):
        if i: out.append("¶")
        for j, part in enumerate(line.split("|TAB|")):
            if j: out.append("Char ( 9 )")
            if part or (not i and not j and len(v) == 0): out.append(fmstr(part))
    return " & ".join(out) or '""'

def lit_date(v):
    return f"Date ( {int(v[5:7])} ; {int(v[8:10])} ; {int(v[0:4])} )"

def lit_time(sec):
    n = num(sec)  # FileMaker renders 0.5 as .5 in calculations
    n = n.replace("-0.", "-.", 1) if n.startswith("-0.") else (n[1:] if n.startswith("0.") else n)
    return f"Time ( 0 ; 0 ; {n} )"

def build_data_script():
    """Straight-line data build. generateDBFile does not link control-flow steps (a false If
    never reaches its End If and the script spins), so the script has no If / Loop: every
    record is an explicit New Record + Set Field sequence and errors are collected with If()."""
    s = S()
    st_comment(s, "Creates the deterministic reference data set (deletes existing records first). "
                  "Run once in FileMaker Pro (single user) so global values are saved.")
    st_errcap(s, True)
    st_setvar(s, "$$BUILD_ERRORS", '""')
    for lay in ("OrderLines", "Orders", "Contacts", "Customers", "Products"):
        st_goto_layout(s, lay); st_showall(s); st_delete_all(s)

    def row(table, n, sets):
        st_new(s)
        for fname, expr, rep in sets:
            st_setfield(s, table, fname, expr, rep=rep)
        st_commit(s)
        st_setvar(s, "$$BUILD_ERRORS", f'If ( Get ( LastError ) ≠ 0 ; List ( $$BUILD_ERRORS ; "{table} row {n} error " & Get ( LastError ) ) ; $$BUILD_ERRORS )')

    def block(table, rows, spec, extra=None):
        st_comment(s, f"{table}: {len(rows)} records")
        st_goto_layout(s, table)
        for n, r in enumerate(rows, 1):
            sets = []
            for name, kind in spec:
                v = r[name]
                if v in (None, ""): continue
                if kind in ("text", "num"): sets.append((name, lit_text(v), None))
                elif kind == "date": sets.append((name, lit_date(v), None))
                elif kind == "time": sets.append((name, lit_time(v), None))
            if extra: sets += extra(r)
            row(table, n, sets)

    def cust_extra(r):
        out = []
        if r["LastContact"]:
            d, sec = r["LastContact"]
            out.append(("LastContact", f"Timestamp ( {lit_date(d)} ; {lit_time(sec)} )", None))
        for k, v in enumerate(r["Tags"], 1):
            if v: out.append(("Tags", lit_text(v), k))
        for k, v in enumerate(r["Scores"], 1):
            if v: out.append(("Scores", lit_text(v), k))
        if r["Photo"]:
            out.append(("Photo", f"Base64Decode ( {fmstr(r['Photo'][1])} ; {fmstr(r['Photo'][2])} )", None))
        return out

    block("Customers", CUST, [("CustomerID", "num"), ("Code", "text"), ("Name", "text"), ("Email", "text"), ("Status", "text"),
          ("Country", "text"), ("Interests", "text"), ("Notes", "text"), ("SecretNote", "text"), ("Pin", "text"),
          ("FavoriteContact", "text"), ("Rating", "num"), ("Balance", "num"), ("LooseNumber", "num"),
          ("SignupDate", "date"), ("PreferredTime", "time"), ("Duration", "time")], cust_extra)
    block("Orders", ORD, [("OrderID", "num"), ("CustomerID", "num"), ("OrderNo", "text"), ("OrderDate", "date"),
          ("Amount", "num"), ("Status", "text"), ("Paid", "num"), ("Comment", "text"), ("FeaturedProduct", "text")])
    block("OrderLines", LIN, [("LineID", "num"), ("OrderID", "num"), ("ProductCode", "text"), ("Description", "text"),
          ("Qty", "num"), ("UnitPrice", "num"), ("Discount", "num")])
    block("Contacts", CONT, [("ContactID", "num"), ("CustomerID", "num"), ("FirstName", "text"), ("LastName", "text"),
          ("Role", "text"), ("Phone", "text"), ("IsPrimary", "num"), ("Birthday", "date")])
    block("Products", PROD, [("ProductID", "num"), ("ProductCode", "text"), ("ProductName", "text"),
          ("Price", "num"), ("Active", "num")])

    st_comment(s, "Global initial values (persist only when set in single-user FileMaker Pro)")
    st_goto_layout(s, "Customers")
    for fname, val in GLOBALS:
        if isinstance(val, list):
            for k, v in enumerate(val, 1): st_setfield(s, "Customers", fname, v, rep=k)
        else:
            st_setfield(s, "Customers", fname, val)
    st_commit(s)
    st_perform(s, "Self Check")
    st_goto_layout(s, "Setup_Report")
    st_exit(s, "$$BUILD_ERRORS")
    return s

GLOBALS = [
    ("gText", '"Global text – é ü 日本"'),
    ("gNumber", '"3.14159265358979"'),
    ("gDate", "Date ( 2 ; 29 ; 2024 )"),
    ("gTime", "Time ( 25 ; 30 ; 15 )"),
    ("gTimestamp", "Timestamp ( Date ( 12 ; 31 ; 1999 ) ; Time ( 23 ; 59 ; 59.5 ) )"),
    ("gContainer", f'Base64Decode ( "{PNG}" ; "global.png" )'),
    ("gRepeat", ['"g-rep-1"', '""', '"g-rep-3"']),
]
GLOBAL_EXPECT = {"gText": "Global text – é ü 日本", "gNumber": "3.14159265358979", "gDate": "2024-02-29",
                 "gTime": "25:30:15", "gTimestamp": "1999-12-31 23:59:59.5", "gContainer": "global.png",
                 "gRepeat": ["g-rep-1", "", "g-rep-3"]}

def self_check_script():
    s = S()
    st_comment(s, "Writes a structure and data summary to Customers::gReport and returns it.")
    tbl = " & ¶ & ".join(f'"count.{t}=" & ExecuteSQL ( "SELECT COUNT(*) FROM \\"{t}\\"" ; "" ; "" )' for t in TABLES)
    sums = ('"sum.OrderLines.Qty=" & ExecuteSQL ( "SELECT SUM(Qty) FROM \\"OrderLines\\"" ; "" ; "" ) & ¶ & '
            '"sum.Orders.Amount=" & ExecuteSQL ( "SELECT SUM(Amount) FROM \\"Orders\\"" ; "" ; "" ) & ¶ & '
            '"distinct.Orders.CustomerID=" & ExecuteSQL ( "SELECT COUNT(DISTINCT CustomerID) FROM \\"Orders\\"" ; "" ; "" ) & ¶ & '
            '"photos=" & ExecuteSQL ( "SELECT COUNT(*) FROM \\"Customers\\" WHERE Photo IS NOT NULL" ; "" ; "" )')
    fc = " & ¶ & ".join(f'"fields.{t}=" & ExecuteSQL ( "SELECT COUNT(*) FROM FileMaker_Fields WHERE TableName = \'{t}\'" ; "" ; "" )' for t in TABLES)
    lf = " & ¶ & ".join(f'"layoutfields.{n}=" & ValueCount ( FieldNames ( Get ( FileName ) ; "{n}" ) )'
                        for n in ["API_Customers", "API_Customers_Portals", "API_Orders", "API_OrderLines", "API_Contacts", "API_Products"])
    text = (f'Let ( r = {tbl} & ¶ & {sums} & ¶ & {fc} & ¶ & {lf} & ¶ & '
            '"layouts=" & Substitute ( LayoutNames ( Get ( FileName ) ) ; ¶ ; "|" ) & ¶ & '
            '"scripts=" & Substitute ( ScriptNames ( Get ( FileName ) ) ; ¶ ; "|" ) & ¶ & '
            '"valuelists=" & Substitute ( ValueListNames ( Get ( FileName ) ) ; ¶ ; "|" ) & ¶ & '
            '"tos=" & Substitute ( TableNames ( Get ( FileName ) ) ; ¶ ; "|" ) & ¶ & '
            '"globals=" & Customers::gText & "|" & Customers::gNumber & "|" & GetAsNumber ( Customers::gDate ) & "|" & GetAsNumber ( Customers::gTime ) & "|" & GetAsNumber ( Customers::gTimestamp ) & "|" & GetAsText ( Customers::gContainer ) & "|" & Customers::gRepeat[3] & ¶ & '
            '"account=" & Get ( AccountName ) & "|" & Get ( AccountPrivilegeSetName ) & ¶ & '
            '"build_errors=" & Substitute ( $$BUILD_ERRORS ; ¶ ; "|" ) ; r )')
    st_setvar(s, "$r", text)
    st_setfield(s, "Customers", "gReport", "$r")
    st_commit(s)
    st_exit(s, "$r")
    return s

def api_scripts():
    d = {}
    s = S(); st_exit(s, '"ok"'); d["API_Success"] = s
    s = S(); st_exit(s, "Get ( ScriptParameter )"); d["API_EchoParam"] = s
    s = S(); st_exit(s, "GetValue ( Get ( ScriptParameter ) ; 2 )"); d["API_SecondParamValue"] = s
    s = S(); st_comment(s, "Last step fails with 401: scriptError must be 401")
    st_errcap(s, True); st_goto_layout(s, "API_Customers"); st_findmode(s)
    st_setfield(s, "Customers", "Code", '"==ZZZ-NONE"'); st_find(s); d["API_Error_NoRecords"] = s
    s = S(); st_comment(s, "Same failing find, then returns Get(LastError) as the result")
    st_errcap(s, True); st_goto_layout(s, "API_Customers"); st_findmode(s)
    st_setfield(s, "Customers", "Code", '"==ZZZ-NONE"'); st_find(s); st_exit(s, "Get ( LastError )")
    d["API_Error_ExitResult"] = s
    s = S(); st_comment(s, "Step not supported by FileMaker Server: expect error 3"); st_textruler(s); d["API_Unsupported"] = s
    s = S(); st_comment(s, "Replaces the found set with Status = Active on a Customers-based layout")
    st_errcap(s, True); st_findmode(s); st_setfield(s, "Customers", "Status", '"==Active"'); st_find(s)
    st_exit(s, "Get ( FoundCount )"); d["API_FindActive"] = s
    s = S(); st_comment(s, "Omits the first record of the current found set (prerequest/presort)")
    st_gotorec(s, "First"); st_omit(s); st_exit(s, "Get ( FoundCount )"); d["API_OmitFirst"] = s
    s = S(); st_sort(s, "Customers", [("Name", "Descending")]); st_exit(s, "Get ( FoundCount )"); d["API_SortByNameDesc"] = s
    s = S(); st_setfield(s, "Customers", "gText", "Get ( ScriptParameter )"); st_commit(s)
    st_exit(s, "Customers::gText"); d["API_SetGlobal"] = s
    s = S(); st_setvar(s, "$$F4B_VAR", "Get ( ScriptParameter )"); st_exit(s, "$$F4B_VAR"); d["API_SetVar"] = s
    s = S(); st_exit(s, "$$F4B_VAR"); d["API_ReadVar"] = s
    s = S(); st_exit(s, '"Zoë – 日本語 – Ελληνικά – עברית – 😀 – \\"quoted\\" – back\\\\slash"'); d["API_UnicodeResult"] = s
    s = S(); st_exit(s, 'List ( Get ( LayoutName ) ; Get ( LayoutTableName ) ; Get ( FoundCount ) ; Get ( RecordNumber ) ; '
                       'Get ( AccountName ) ; Get ( AccountPrivilegeSetName ) ; "param=" & Get ( ScriptParameter ) )')
    d["API_ReturnContext"] = s
    d["Build Reference Data"] = build_data_script()
    d["Self Check"] = self_check_script()
    if os.environ.get("F4B_DEBUG"):  # control-flow probes (generateDBFile linkage)
        t = S(); st_if(t, "0"); st_setvar(t, "$x", '"inside"'); st_endif(t); st_exit(t, '"iffalse ok"'); d["DBG_IfFalse"] = t
        t = S(); st_if(t, "1"); st_setvar(t, "$x", '"inside"'); st_endif(t); st_exit(t, '"iftrue ok " & $x'); d["DBG_IfTrue"] = t
        t = S(); st_setvar(t, "$i", "0"); st_loop(t); st_setvar(t, "$i", "$i + 1"); st_exitloopif(t, "$i > 3"); st_endloop(t)
        st_exit(t, '"loop ok " & $i'); d["DBG_Loop"] = t
    return d

# --------------------------------------------------------------------------- catalog emitters
def field_xml(f):
    k = f.kw
    reps = k.get("reps", 1)
    cm = q(k.get("comment", ""))
    head = f'<Field id="{f.id}" name="{q(f.name)}" fieldtype="{f.kind}" datatype="{f.dt}" comment="{cm}">'
    if f.kind == "Summary":
        of = k["of"]
        return (head + '<AutoEnter type="" prohibitModification="False"></AutoEnter>'
                f'<SummaryInfo restartEachGroup="False" summarizeRepetition="Together" operation="{k["op"]}">'
                f'<SummaryField>{fref_bt(f.table, of)}</SummaryField></SummaryInfo><TagList></TagList></Field>')
    if f.kind == "Calculated":
        stored = k.get("stored", False)
        lang = LANG if f.dt != "Binary" else ""
        stor = (f'<Storage storeCalculationResults="{stored}"' + (' autoIndex="True" index="None"' if stored and f.dt != "Binary" else "")
                + f' global="False" maxRepetitions="{reps}">{lang}</Storage>')
        return (head + '<AutoEnter alwaysEvaluate="False"></AutoEnter>' + stor +
                f'<Calculation>{toref(f.table)}<Text>{cdata(k["calc"])}</Text></Calculation><TagList></TagList></Field>')
    # Normal
    if k.get("serial"):
        ae = '<AutoEnter type="SerialNumber" prohibitModification="False"><SerialNumber increment="1" nextvalue="1" generate="OnCreation"></SerialNumber></AutoEnter>'
    elif k.get("ae"):
        ae = f'<AutoEnter type="{k["ae"]}" prohibitModification="True"></AutoEnter>'
    elif k.get("aeconst"):
        ae = f'<AutoEnter type="ConstantData" prohibitModification="False"><ConstantData>{q(k["aeconst"])}</ConstantData></AutoEnter>'
    else:
        ae = '<AutoEnter type="" prohibitModification="False"></AutoEnter>'
    always = k.get("always", False)
    vattrs = (f'alwaysValidate="False" type="{"Always" if always else "OnlyDuringDataEntry"}" '
              f'allowOverride="{"False" if always else "True"}" notEmpty="{k.get("notEmpty", False)}" '
              f'unique="{k.get("unique", False)}" existing="False"')
    vch = ""
    if k.get("strict"): vch += f'<Strict>{k["strict"]}</Strict>'
    if k.get("rng"): vch += f'<Range from="{k["rng"][0]}" to="{k["rng"][1]}"></Range>'
    if k.get("maxsize"): vch += f'<MaximumSize>{k["maxsize"]}</MaximumSize>'
    if k.get("vlist"): vch += vlref(k["vlist"])
    if k.get("vcalc"):
        vch += f'<Calculated enable="True"><Calculation>{toref(f.table)}<Text>{cdata(k["vcalc"])}</Text></Calculation></Calculated>'
    if k.get("vmsg"): vch += f'<Message>{q(k["vmsg"])}</Message>'
    val = f'<Validation {vattrs}>{vch}</Validation>'
    g = k.get("glob", False)
    if f.dt == "Binary":
        stor = f'<Storage global="{g}" maxRepetitions="{reps}"></Storage>'
    else:
        if g:
            stor = f'<Storage global="True" maxRepetitions="{reps}">{LANG}</Storage>'
        elif k.get("index"):
            stor = f'<Storage index="{k["index"]}" global="False" maxRepetitions="{reps}">{LANG}</Storage>'
        else:
            idx = "Minimal" if k.get("unique") else "None"
            stor = f'<Storage autoIndex="True" index="{idx}" global="False" maxRepetitions="{reps}">{LANG}</Storage>'
    return head + ae + val + stor + '<TagList></TagList></Field>'

def field_pass(f):
    """Creation pass. FMUpgradeTool resolves names at creation time (no UUIDs here), so a
    definition must come after everything it references; unresolved calcs get commented out.
    1 plain fields; 2 calcs/summaries on own table; 3 calcs through relationships;
    4 fields validated by a value list (after the value list catalog)."""
    if f.kw.get("vlist"):
        return 4
    if f.kind == "Calculated" and "::" in f.kw["calc"]:
        return 3
    if f.kind in ("Calculated", "Summary"):
        return 2
    return 1

def fields_catalog(npass):
    fc = []
    for t, T in TABLES.items():
        fl = [f for f in T["fields"] if field_pass(f) == npass]
        if fl:
            fc.append(f'<FieldCatalog>{btref(t)}<ObjectList membercount="{len(fl)}">' + "".join(field_xml(f) for f in fl) + '</ObjectList></FieldCatalog>')
    return f'<FieldsForTables membercount="{len(fc)}">{"".join(fc)}</FieldsForTables>'

def relationship_xml(i, r):
    lt, lf, rt, rf, o = r
    def side(tag, to, create, delete, sort):
        sx = ""
        if sort:
            sl = "".join(f'<Sort type="{d}"><PrimaryField>{fref_to(to, f)}</PrimaryField></Sort>' for f, d in sort)
            sx = f'<SortSpecification value="True" blanksLast="False" maintain="True"><SortList membercount="{len(sort)}">{sl}</SortList></SortSpecification>'
        return f'<{tag} cascadeCreate="{create}" cascadeDelete="{delete}" type="Local">{toref(to)}{sx}</{tag}>'
    return (f'<Relationship id="{i}">' + side("LeftTable", lt, False, False, None) +
            side("RightTable", rt, o.get("rcreate", False), o.get("rdelete", False), o.get("rsort")) +
            f'<JoinPredicateList membercount="1"><JoinPredicate type="Equal"><LeftField>{fref_to(lt, lf)}</LeftField>'
            f'<RightField>{fref_to(rt, rf)}</RightField></JoinPredicate></JoinPredicateList></Relationship>')

def valuelist_xml(i, v):
    n, kind, d = v
    if kind == "custom":
        return (f'<ValueList id="{i}" name="{q(n)}"><Source value="Custom"></Source>'
                f'<CustomValues><Text>{cdata(chr(13).join(d))}</Text></CustomValues></ValueList>')
    sec = ""
    if d["secondary"]:
        sec = f'<SecondaryField show="{d["sshow"]}" sort="{d["sshow"]}">{fref_to(d["to"], d["secondary"])}</SecondaryField>'
    rel = f'<ShowRelated value="True">{toref(d["related"])}</ShowRelated>' if d["related"] else '<ShowRelated value="False"></ShowRelated>'
    return (f'<ValueList id="{i}" name="{q(n)}"><Source value="FromField"></Source><Field>'
            f'<PrimaryField show="{d["pshow"]}" sort="{d["pshow"]}">{fref_to(d["to"], d["primary"])}</PrimaryField>{sec}{rel}</Field></ValueList>')

def script_catalog_xml():
    out = []
    for i, (k, n) in enumerate(SCRIPTS, 1):
        attr = {"folder": ' isFolder="True"', "end": ' isFolder="Marker"', "sep": ' isSeparatorItem="True"'}.get(k, "")
        hidden, val = {"folder": ("True", 13), "end": ("False", 2), "sep": ("True", 24)}.get(k, ("False", 0))
        out.append(f'<Script id="{i}" name="{q(n)}"{attr}><Options hidden="{hidden}" access="ReadWrite" SiriShortcutVisible="False" '
                   f'runwithfullaccess="False" compatibility="0">{val}</Options><TagList></TagList></Script>')
    return f'<ScriptCatalog membercount="{len(out)}">{"".join(out)}</ScriptCatalog>'

def steps_xml(bodies):
    out = []
    for n, sid in SCRIPT_ID.items():
        s = bodies[n]
        st = []
        for idx, (stid, sname, opts, params) in enumerate(s.steps):
            pv = ""
            if params:
                pv = f'<ParameterValues membercount="{params.count("<Parameter type=")}">{params}</ParameterValues>'
            st.append(f'<Step index="{idx}" id="{stid}" name="{q(sname)}" enable="True"><UUID></UUID><OwnerID></OwnerID>'
                      f'<Options>{opts}</Options>{pv}</Step>')
        out.append(f'<Script><ScriptReference id="{sid}" name="{q(n)}"></ScriptReference>'
                   f'<ObjectList membercount="{len(st)}">{"".join(st)}</ObjectList></Script>')
    return f'<StepsForScripts membercount="{len(out)}">{"".join(out)}</StepsForScripts>'

# Privilege sets
DEFAULT_PS = '''<PrivilegeSet id="1" name="[Full Access]"><Description>access to everything</Description><access default="True"><Records Create="True" Edit="True" Delete="True"></Records><Layouts Create="True" Edit="True" Delete="True"></Layouts><ValueLists Create="True" Edit="True" Delete="True"></ValueLists><Scripts Create="True" Edit="True" Delete="True"></Scripts><Other value="65535" Print="True" Export="True" manageDatabase="True" manageCustomMenus="True" manageAccounts="True" manageExtPrivs="True" allowOverride="True" allowOpenQuickly="True" disconnectIdle="False" commands="All"><Password prohibitModification="False"></Password></Other></access></PrivilegeSet><PrivilegeSet id="2" name="[Data Entry Only]"><Description>write access to all records, no design access</Description><access default="True"><Records Create="True" Edit="True" Delete="True"></Records><Layouts View="ReadOnly"></Layouts><ValueLists View="ReadOnly"></ValueLists><Scripts View="ReadOnly"></Scripts><Other value="25348" Print="True" Export="True" manageDatabase="False" manageCustomMenus="False" manageAccounts="False" manageExtPrivs="False" allowOverride="False" allowOpenQuickly="False" disconnectIdle="True" commands="All"><Password prohibitModification="False"></Password></Other></access></PrivilegeSet><PrivilegeSet id="3" name="[Read-Only Access]"><Description>read access to all records, no design access, write access to all global fields</Description><access default="True"><Records View="ReadOnly"></Records><Layouts View="ReadOnly"></Layouts><ValueLists View="ReadOnly"></ValueLists><Scripts View="ReadOnly"></Scripts><Other value="25348" Print="True" Export="True" manageDatabase="False" manageCustomMenus="False" manageAccounts="False" manageExtPrivs="False" allowOverride="False" allowOpenQuickly="False" disconnectIdle="True" commands="All"><Password prohibitModification="False"></Password></Other></access></PrivilegeSet>'''

OTHER_USER = ('<Other value="25348" Print="True" Export="True" manageDatabase="False" manageCustomMenus="False" '
              'manageAccounts="False" manageExtPrivs="False" allowOverride="False" allowOpenQuickly="False" '
              'disconnectIdle="True" commands="All"><Password prohibitModification="False"></Password></Other>')

def layouts_custom(noaccess, records_default):
    items = []
    for i, L in enumerate(LAYOUTS, 1):
        if L[0] not in ("layout", "sep"): continue
        if L[1] in noaccess:
            items.append(f'<Layout type="existing" access="NoAccess" records="NoAccess"><LayoutReference id="{i}" name="{q(L[1])}"></LayoutReference></Layout>')
        else:
            items.append(f'<Layout type="existing" access="ReadOnly" records="{records_default}"><LayoutReference id="{i}" name="{q(L[1])}"></LayoutReference></Layout>')
    return f'<Layouts Custom="True"><Custom Create="False"><ObjectList membercount="{len(items)}">{"".join(items)}</ObjectList></Custom></Layouts>'

def records_custom_rw(hidden):
    items = []
    for t, T in TABLES.items():
        if any(h[0] == t for h in hidden):
            fl = "".join(f'<Field type="existing" access="{"NoAccess" if (t, f.name) in hidden else "ReadWrite"}">{fref_bt(t, f.name)}</Field>' for f in T["fields"])
            fx = f'<Fields access="Custom">{fl}</Fields>'
        else:
            fx = '<Fields access="ReadWrite"></Fields>'
        items.append(f'<Table type="existing">{btref(t)}<View access="ReadWrite"></View><Edit access="ReadWrite"></Edit>'
                     f'<Create access="ReadWrite"></Create><Delete access="ReadWrite"></Delete>{fx}</Table>')
    return f'<Records Custom="True"><Custom><ObjectList membercount="{len(items)}">{"".join(items)}</ObjectList></Custom></Records>'

CUSTOM_PS = [
    (4, "DataAPI_ReadWrite", "Data API read/write; Customers::SecretNote and layout API_NoAccess have no access",
     records_custom_rw({("Customers", "SecretNote")}) + layouts_custom({"API_NoAccess"}, "ReadWrite") +
     '<ValueLists View="ReadOnly"></ValueLists><Scripts View="ReadOnly"></Scripts>' + OTHER_USER),
    (5, "DataAPI_ReadOnly", "Data API read-only; layout API_NoAccess has no access",
     '<Records View="ReadOnly"></Records>' + layouts_custom({"API_NoAccess"}, "ReadOnly") +
     '<ValueLists View="ReadOnly"></ValueLists><Scripts View="ReadOnly"></Scripts>' + OTHER_USER),
    (6, "NoAPI_DataEntry", "Data entry without the fmrest extended privilege",
     '<Records Create="True" Edit="True" Delete="True"></Records><Layouts View="ReadOnly"></Layouts>'
     '<ValueLists View="ReadOnly"></ValueLists><Scripts View="ReadOnly"></Scripts>' + OTHER_USER),
]
PSNAME = {1: "[Full Access]", 2: "[Data Entry Only]", 3: "[Read-Only Access]", 4: "DataAPI_ReadWrite",
          5: "DataAPI_ReadOnly", 6: "NoAPI_DataEntry"}

EXT = [(1, "fmwebdirect", "Access via FileMaker WebDirect", []),
       (2, "fmxdbc", "Access via ODBC/JDBC", []),
       (3, "fmapp", "Access via FileMaker Network", [1, 6]),
       (4, "fmreauthenticate10", "Require re-authentication after the specified minutes in sleep/background.", [1, 2, 3]),
       (5, "fmxml", "Access via XML Web Publishing - FMS only", []),
       (6, "fmphp", "Access via PHP Web Publishing - FMS only", []),
       (7, "fmextscriptaccess", "Allow Apple events and ActiveX to perform FileMaker operations", []),
       (8, "fmurlscript", "Allow URLs to run FileMaker scripts", []),
       (9, "fmrest", "Access via FileMaker Data API", [1, 4, 5]),
       (10, "fmodata", "Access via OData - FMS only", []),
       (11, "fmplugin", "Allow plug-ins to be installed and updated", [])]

ACCOUNTS = [(2, "admin", 1, "Full Access lab administrator"), (3, "api_rw", 4, "Data API read/write"),
            (4, "api_ro", 5, "Data API read-only"), (5, "noapi", 6, "No fmrest extended privilege")]

def build_xml(ext_desc):
    parts = []
    parts.append(f'<BaseTableCatalog membercount="{len(TABLES)}">' + "".join(
        f'<BaseTable id="{T["id"]}" comment="" name="{q(t)}"></BaseTable>' for t, T in TABLES.items()) + '</BaseTableCatalog>')
    tos = []
    for k, (n, bt) in enumerate(TOS):
        top = 20 + (k % 5) * 150; left = 20 + (k // 5) * 260
        tos.append(f'<TableOccurrence View="Full" height="120" id="{TO[n]["id"]}" name="{q(n)}" type="Local">'
                   f'<BaseTableSourceReference type="BaseTableReference">{btref(bt)}</BaseTableSourceReference>'
                   f'<CoordRect top="{top}" left="{left}" bottom="{top + 120}" right="{left + 200}"></CoordRect></TableOccurrence>')
    parts.append(f'<TableOccurrenceCatalog membercount="{len(tos)}">{"".join(tos)}</TableOccurrenceCatalog>')
    parts.append(fields_catalog(1))
    parts.append(f'<RelationshipCatalog membercount="{len(RELS)}">' + "".join(relationship_xml(i, r) for i, r in enumerate(RELS, 1)) + '</RelationshipCatalog>')
    parts.append(fields_catalog(2))
    parts.append(fields_catalog(3))
    parts.append(f'<ValueListCatalog membercount="{len(VLISTS)}">' + "".join(valuelist_xml(i, v) for i, v in enumerate(VLISTS, 1)) + '</ValueListCatalog>')
    parts.append(fields_catalog(4))
    parts.append(script_catalog_xml())
    parts.append(f'<LayoutCatalog membercount="{len(LAYOUTS)}">' + "".join(layout_xml(i, L) for i, L in enumerate(LAYOUTS, 1)) + '</LayoutCatalog>')
    ps = DEFAULT_PS + "".join(f'<PrivilegeSet id="{i}" name="{q(n)}"><Description>{q(d)}</Description><access>{a}</access></PrivilegeSet>' for i, n, d, a in CUSTOM_PS)
    parts.append(f'<PrivilegeSetsCatalog membercount="{3 + len(CUSTOM_PS)}"><ObjectList membercount="{3 + len(CUSTOM_PS)}">{ps}</ObjectList></PrivilegeSetsCatalog>')
    ex = []
    for i, n, d, sets in EXT:
        ol = ""
        if sets:
            ol = f'<ObjectList membercount="{len(sets)}">' + "".join(f'<PrivilegeSetReference id="{s}" name="{q(PSNAME[s])}"></PrivilegeSetReference>' for s in sets) + '</ObjectList>'
        ex.append(f'<ExtendedPrivilege id="{i}" name="{n}"><Description>{q(d)}</Description><TagList></TagList>{ol}</ExtendedPrivilege>')
    parts.append(f'<ExtendedPrivilegesCatalog membercount="{len(EXT)}"><ObjectList membercount="{len(EXT)}">{"".join(ex)}</ObjectList></ExtendedPrivilegesCatalog>')
    acc = "".join(f'<Account id="{i}" kind="0" type="FileMaker" enable="True"><TagList></TagList><Description>{q(d)}</Description>'
                  f'<Authentication><AccountName>{n}</AccountName></Authentication><PrivilegeSetReference id="{p}" name="{q(PSNAME[p])}"></PrivilegeSetReference></Account>'
                  for i, n, p, d in ACCOUNTS)
    parts.append(f'<AccountsCatalog membercount="{len(ACCOUNTS)}"><Options>0</Options><ObjectList membercount="{len(ACCOUNTS)}">{acc}</ObjectList></AccountsCatalog>')
    parts.append(steps_xml(api_scripts()))
    body = "".join(parts)
    return ('<?xml version="1.0" encoding="UTF-8"?>\n'
            f'<FMSaveAsXML version="2.3.0.0" Source="26.0.2" File="{FILE}.fmp12" locale="English" Has_DDR_INFO="False" binary_under_lo="False" split_catalogs="False">'
            f'<Structure membercount="1"><AddAction membercount="{len(parts)}">{body}</AddAction></Structure></FMSaveAsXML>\n')

def expected():
    strip = lambda rows: [{k: v for k, v in r.items() if not k.startswith("_")} for r in rows]
    return {"file": FILE + ".fmp12",
            "notes": "Values are the literal inputs set by 'Build Reference Data'. |NL| = carriage return, |TAB| = tab. "
                     "Times/durations are seconds; LastContact is [date, seconds]; Photo is [kind, base64, filename].",
            "counts": {"Customers": len(CUST), "Orders": len(ORD), "OrderLines": len(LIN),
                       "Contacts": len(CONT), "Products": len(PROD)},
            "globals": GLOBAL_EXPECT,
            "layouts": [{"id": i, "kind": L[0], "name": L[1], "to": L[2] if L[0] in ("layout", "sep") else None}
                        for i, L in enumerate(LAYOUTS, 1)],
            "scripts": [{"id": i, "kind": k, "name": n} for i, (k, n) in enumerate(SCRIPTS, 1)],
            "valueLists": [{"id": i, "name": n, "kind": k, "values": d if k == "custom" else None}
                           for i, (n, k, d) in enumerate(VLISTS, 1)],
            "accounts": [{"name": n, "privilegeSet": PSNAME[p]} for _, n, p, _ in ACCOUNTS],
            "Customers": strip(CUST), "Orders": ORD, "OrderLines": LIN, "Contacts": CONT, "Products": PROD}

if __name__ == "__main__":
    outdir = sys.argv[1] if len(sys.argv) > 1 else os.path.join(os.path.dirname(os.path.abspath(__file__)), "build")
    os.makedirs(outdir, exist_ok=True)
    with open(os.path.join(outdir, FILE + ".xml"), "w", encoding="utf-8") as fh:
        fh.write(build_xml(None))
    with open(os.path.join(outdir, "expected-data.json"), "w", encoding="utf-8") as fh:
        json.dump(expected(), fh, ensure_ascii=False, indent=1)
    print("wrote", outdir, "customers", len(CUST), "orders", len(ORD), "lines", len(LIN),
          "contacts", len(CONT), "products", len(PROD))
