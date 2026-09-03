# ImapMan: Frontend-Architektur und API-Referenz

Dieses Dokument ist die Umsetzungsgrundlage fuer eine separate Next.js-App
(App Router, TypeScript). Die App ist ein reines Verwaltungsfrontend fuer den
ImapMan-Go-Dienst; der Go-Dienst verarbeitet Mails autonom.

## Zielarchitektur

```text
Browser
  -> Next.js UI und Server Actions / Route Handlers
       -> ImapMan REST API (/api/v1, Bearer Secret)
            -> MariaDB
            -> IMAP-Postfaecher und SMTP-Server
```

Das Secret darf nicht mit `NEXT_PUBLIC_` beginnen und nie an den Browser
gelangen. Es gehoert ausschliesslich in die Server-Umgebung der Next.js-App:

```env
IMAPMAN_API_URL=http://imapman:8080
IMAPMAN_API_SECRET=<gleich wie im ImapMan-Container>
```

Erstelle einen serverseitigen API-Client, der fuer jede Anfrage den Header
`Authorization: Bearer ${process.env.IMAPMAN_API_SECRET}` setzt. Bei
POST- und PUT-Anfragen muss zudem `Content-Type: application/json` gesetzt
werden. Alle `/api/v1`-Routen verlangen diesen Header. `/healthz` und
`/openapi.yaml` sind oeffentlich.

Der API-Client soll bei Nicht-2xx die JSON-Antwort
`{"error":"..."}` auswerten und als benutzerfreundlichen Formular- oder
Toast-Fehler darstellen. `204 No Content` hat keinen Response-Body.

## Domänenmodell und Verknuepfungen

```text
Mailbox --1:1--> Distributor --n:m--> MailingList --1:n--> Member
                         |
                         +--n:m--> Sender-MailingList

MailingList --0:1--> DataSource
```

| Entitaet | Zweck | Wichtige Regeln |
|---|---|---|
| `Mailbox` | Einzeln konfiguriertes IMAP-Postfach, optional eigener SMTP-Transport | Hat hoechstens einen Verteiler. |
| `Distributor` | Verteilt die Mail eines Postfachs an seine Ziellisten | Legt die Absenderautorisierung fest. |
| `MailingList` | Statische oder externe Empfaenger-/Absenderliste | Darf Zielliste und Senderliste zugleich sein. |
| `Member` | Manuell gepflegter Eintrag einer statischen Liste | `receives_mail` gilt nur fuer den Empfang von Verteilermails. |
| `DataSource` | Gespeicherte Verbindung zu externer PostgreSQL-, MySQL- oder SQLite-Datenbank | DSN wird nach dem Anlegen niemals ueber die API ausgegeben. |
| `ProcessedMessage` | Audit- und Statusdaten einer IMAP-UID | Nur lesbar. |

Eine `Mailbox` wird per Zuordnung genau einem `Distributor` zugewiesen. Ein
`Distributor` kann mehrere Ziellisten enthalten, eine `MailingList` kann an
mehreren Verteilern haengen. Das gilt ebenso fuer die Senderlisten.

Beim Loeschen eines Postfachs werden dessen Zuordnung und
Verarbeitungsstatus geloescht. Beim Loeschen eines Verteilers werden
Zuordnungen geloescht. Beim Loeschen einer Liste werden Member und alle
Zuordnungen geloescht. Eine noch von einer Datenbankliste referenzierte
Datenquelle kann nicht geloescht werden.

## Empfohlene Seiten und Navigation

1. **Dashboard**: Anzahl Postfaecher, Verteiler, Listen, fehlgeschlagene
   Nachrichten; Tabelle der letzten `processed-messages`.
2. **Postfaecher**: Liste, Erstellen, Loeschen und Verteiler zuweisen.
3. **Verteiler**: Liste, Erstellen, Bearbeiten, Loeschen, Ziellisten und
   Senderlisten verknuepfen.
4. **Mailinglisten**: Liste, statische oder Datenbankliste erstellen,
   Loeschen, Versand-Override bearbeiten und statische Member verwalten.
5. **Datenquellen**: Liste, erstellen und loeschen.
6. **Verarbeitungsstatus**: Tabelle aus `GET /processed-messages`, mit
   Status-Badges fuer `processing`, `sent`, `failed` und `rejected`.

Da die API keine `GET /{id}`-Routen besitzt, wird eine Detailseite mit den
bereits geladenen Objekten oder per erneuten Listenabruf versorgt. Es gibt
ausser bei Verteilern keine Update-Routen. Formulare fuer Postfach, Liste
und Datenquelle sind daher gegenwaertig Create-only; fuer Aenderungen muss
das Objekt neu angelegt und umverknuepft werden.

## Typen fuer TypeScript

```ts
export type SenderPolicy =
  | "allow_all"
  | "whitelist"
  | "blacklist"
  | "members_only";

export type DeliverySettings = {
  batch_size: number;
  use_bcc: boolean;
  to_header: "from" | "undisclosed";
  delay_seconds: number;
};

export type Mailbox = {
  id: number;
  name: string;
  imap_host: string;
  imap_port: number;
  imap_username: string;
  imap_password_env: string;
  imap_folder: string;
  imap_mark_seen: boolean;
  smtp_host?: string;
  smtp_port?: number;
  smtp_username?: string;
  smtp_password_env?: string;
  smtp_from?: string;
};

export type Distributor = {
  id: number;
  name: string;
  sender_policy: SenderPolicy;
};

export type DataSource = {
  id: number;
  name: string;
  driver: "postgres" | "mysql" | "sqlite";
  username?: string;
  password_env?: string;
};

export type Condition = {
  column: string;
  op: "=" | "!=" | "<" | "<=" | ">" | ">=" | "LIKE";
  value: string | number | boolean;
};

export type MailingList = {
  id: number;
  name: string;
  list_type: "static" | "database";
  datasource_id?: number;
  recipient_table?: string;
  email_column?: string;
  name_column?: string;
  filter?: Condition[];
  delivery?: DeliverySettings;
};

export type Member = {
  id: number;
  mailing_list_id: number;
  name: string;
  email: string;
  receives_mail: boolean;
  member_since: string;
};
```

## API-Routen

Alle folgenden Routen haben den Prefix `/api/v1`.

| Methode | Route | Request-Body | Verwendung |
|---|---|---|---|
| GET | `/mailboxes` | - | Alle Postfaecher laden |
| POST | `/mailboxes` | `Mailbox` ohne `id` | Postfach erstellen |
| DELETE | `/mailboxes/{mailboxID}` | - | Postfach loeschen |
| PUT | `/mailboxes/{mailboxID}/distributor/{distributorID}` | - | Verteiler setzen oder ersetzen |
| DELETE | `/mailboxes/{mailboxID}/distributor/{distributorID}` | - | Zuordnung entfernen |
| GET | `/distributors` | - | Alle Verteiler laden |
| POST | `/distributors` | `Distributor` ohne `id` | Verteiler erstellen |
| PUT | `/distributors/{distributorID}` | `{name, sender_policy}` | Verteiler aktualisieren |
| DELETE | `/distributors/{distributorID}` | - | Verteiler loeschen |
| GET | `/lists` | - | Alle Listen laden |
| POST | `/lists` | `MailingList` ohne `id` | Liste erstellen |
| DELETE | `/lists/{listID}` | - | Liste loeschen |
| PUT | `/distributors/{distributorID}/lists/{listID}` | - | Zielliste verknuepfen |
| DELETE | `/distributors/{distributorID}/lists/{listID}` | - | Ziellisten-Verknuepfung entfernen |
| PUT | `/distributors/{distributorID}/sender-lists/{listID}` | - | Senderliste verknuepfen |
| DELETE | `/distributors/{distributorID}/sender-lists/{listID}` | - | Senderlisten-Verknuepfung entfernen |
| GET | `/lists/{listID}/members` | - | Statische Member laden |
| POST | `/lists/{listID}/members` | `Member` ohne IDs und `member_since` | Einzelnen Member erstellen |
| DELETE | `/lists/{listID}/members/{memberID}` | - | Einzelnen Member loeschen |
| GET | `/data-sources` | - | Datenquellen ohne DSN laden |
| POST | `/data-sources` | `DataSource` plus `dsn` | Datenquelle erstellen |
| DELETE | `/data-sources/{dataSourceID}` | - | Unreferenzierte Datenquelle loeschen |
| GET | `/processed-messages` | - | Verarbeitungsstatus laden |

## Formulare und Validierung

### Postfach

Pflichtfelder: `name`, `imap_host`, `imap_username`, `imap_password_env`,
`imap_folder`. `imap_port` ist optional und wird auf `993` gesetzt.
`imap_mark_seen` ist ein Boolean. Ein eigener SMTP-Transport muss als
vollstaendiger Block eingegeben werden: `smtp_host`, `smtp_port`,
`smtp_username`, `smtp_password_env`, `smtp_from`. Ohne diesen Block gilt
der globale SMTP-Transport des Go-Dienstes.

Passwortwerte werden nicht ueber das Formular uebergeben. Die Felder
`*_password_env` enthalten den Namen einer Umgebungsvariable im
ImapMan-Container, beispielsweise `TEAM_IMAP_PASSWORD`.

### Verteiler und Absenderpolitik

`sender_policy` steuert, welche Absender verteilt werden duerfen:

| Wert | Verhalten im UI |
|---|---|
| `allow_all` | Keine Senderliste erforderlich. |
| `whitelist` | Mindestens eine Senderliste ueber `sender-lists` verknuepfen. Nur Treffer duerfen senden. |
| `blacklist` | Senderlisten verknuepfen. Treffer werden abgelehnt. |
| `members_only` | Keine separate Senderliste. Es gelten alle Ziel-Mailinglisten des Verteilers als erlaubte Absender. |

Die UI sollte bei `whitelist` und `blacklist` einen Picker fuer vorhandene
Listen anzeigen. Senderlisten duerfen sowohl `static` als auch `database`
sein. Bei `members_only` sollte sie die Ziellisten anzeigen und den
Senderlisten-Picker ausblenden.

### Mailingliste

Eine `static`-Liste verwendet einzeln angelegte Member. Eine `database`-Liste
benoetigt `datasource_id`, `recipient_table` und `email_column`; optional
sind `name_column` und `filter`. `filter` ist eine Liste konfigurierbarer,
aber begrenzter Bedingungen. Erlaube im UI nur die Operatoren aus `Condition`
und nur Bezeichner ohne Sonderzeichen. Der Go-Dienst validiert und
parametrisiert dennoch jede Bedingung.

Das optionale `delivery`-Objekt ueberschreibt globale Versandwerte nur fuer
diese Liste. Wird es weggelassen, gelten die Werte aus ImapMan `.env`.
`batch_size` ist mindestens 1. Bei `use_bcc: false` erfolgt sichere
Einzelzustellung. `to_header: "from"` zeigt die Absenderadresse, `"undisclosed"`
setzt `To: undisclosed-recipients:;`. Zieladressen werden nie in
To/Cc/Bcc-Header geschrieben.

### Statische Member

Beim Anlegen sind `name` und `email` Pflicht. `receives_mail` sollte im UI
standardmaessig `true` sein. Ein `false`-Member bleibt fuer Whitelist,
Blacklist und `members_only` ein gueltiger Absender, empfaengt aber keine
Verteilermails.

## Empfohlene Server-Action-Struktur

```ts
// lib/imapman.ts - nur in Server Components, Server Actions oder Route Handlers
export async function imapmanFetch(path: string, init: RequestInit = {}) {
  const response = await fetch(`${process.env.IMAPMAN_API_URL}/api/v1${path}`, {
    ...init,
    headers: {
      Authorization: `Bearer ${process.env.IMAPMAN_API_SECRET}`,
      "Content-Type": "application/json",
      ...init.headers,
    },
    cache: "no-store",
  });
  if (!response.ok) {
    const body = await response.json().catch(() => ({}));
    throw new Error(body.error ?? `ImapMan API: ${response.status}`);
  }
  return response.status === 204 ? undefined : response.json();
}
```

Nach erfolgreichen Mutationen `revalidatePath()` fuer die betroffene Liste,
den Verteiler oder das Dashboard aufrufen. Loeschaktionen brauchen einen
Confirm-Dialog mit dem Namen des Objekts und dem Hinweis auf die oben
beschriebenen Kaskaden.

## Betriebs- und Sicherheitsvorgaben

- Das API-Secret ist ein gemeinsames Deployment-Secret und muss in Next.js
  und ImapMan identisch sein.
- Das Frontend darf weder `IMAPMAN_API_SECRET` noch Verbindungs-DSNs an
  Browser-Code, Logs oder Client State ausgeben.
- Die API gibt Datenquellen absichtlich ohne DSN zurueck. Nach einem
  erfolgreichen POST ist eine vorhandene DSN im Client State zu verwerfen.
- Fuer Produktion ImapMan nicht direkt aus dem Internet bereitstellen;
  Zugriff auf die API durch Netzwerk, Reverse Proxy oder Next.js-Backend
  begrenzen.
- Der vollständige maschinenlesbare Vertrag liegt auf
  `http://<imapman>/openapi.yaml`.
