
# router
the new router feature allows a sender to distribute signals to multiple isns that share a common signal type without calling each isn signals endpoint separately.  The route is determined based on a configurable data item contained in the signal
Setup routes | Signal Type Slug | Mapping Field | Pattern |Target ISN
prenotification | payload.portOfEntry | (?i).*felixstowe.* | felixstowe
prenotification | payload.portOfEntry | (?i).*teesside.* | teesside


The pattern is a regular expression.  Setup is done by site admin.
New endpoint for senders
Clients that want to use the router send to 
POST /api/router/signal-types/{signal_type_slug}/v{sem_ver}/signals
... this avoids making router a reserved word when createing signals

… rather than posting directly to the ISN as is done today 
post /api/isn/{isn}/signal-types/{signal_type_slug}/v{sem_ver}/signals)
Server handling
The handler for the /api/router endpoints applies each rule in turn to each of the supplied signals.  The first match is used to determine the route, and if no rules match the signal is rejected.


# Auth
- Validate access to the isn lazily in the handler per-signal after routing resolution 


# Match approach - gjson

Match-any — route fires if any value in the array matches the pattern. Most useful, natural semantics for "does this request contain an item of type X".

gjson has a @ modifier syntax (@reverse, @flatten, etc.) and a pipe operator that are powerful but will be potentially confusing for non tech users - we will support patterns like this for advanced use cases, but only document simple pattern matching examples, for example:


Matching text that contains a word or phrase
To match any value containing felixstowe, use .* before and after your term:
.*felixstowe.*
The .* means "any characters" — so this matches port of felixstowe, felixstowe dock and so on.
By default, matching is case sensitive. To make your pattern ignore capitalisation, add (?i) at the start:
(?i).*felixstowe.*
This will then also match Port of Felixstowe, FELIXSTOWE and so on.
Common patterns
I want to match...PatternCase insensitiveText containing a word.*felixstowe.*(?i).*felixstowe.*Text starting with a wordfelixstowe.*(?i)felixstowe.*Text ending with a word.*felixstowe(?i).*felixstoweExact text onlyfelixstowe(?i)felixstoweEither of two words.*felixstowe.*|.*harwich.*(?i).*felixstowe.*|.*harwich.*

# new UI route management form
temmpl based UI form  - not this needs to handle route sequencing -

# route sequencing
the first matching route is applied to determine the endpoint.  The user therefore needs a way to manage the sequence of the rules.


# Signal Correlations
sendin correlated signals must be handled differently.  In this case the service already knows which isn/signal type is used by the original signal.
One option is to have an option to route by correlated signal id - the database could then look up the correct isn/signal type based on the supplied correlation id.

# route caching
the route cache is managed as follows

- startup          → load rules from DB, compile router
- user saves rules in the new UI route managemnt form → persist to DB, trigger recompile, swap router
- server restart   → load rules from DB again (same as startup)

cache on startup - there should be an on demand feature to refresh the cache while the service is running, as is done elsewhere in signalsd. this will be called from the UI form needed to add routes.


# Limitations
The routers only work for ISNs deployed on the same site.
You can’t mix signal type versions - this means all ISNs need to support the signal type version being targeted.
Where data is delivered incrementally using a single signal type definition, all records must contain the same mapping field

# Searching across ISNs
Rather than searching separately, e.g
get /api/isn/sudan/signal-types/medical-supplies/v1.0.0/signals 
get /api/isn/lebanon/signal-types/medical-supplies/v1.0.0/signals 

You can:
GET  /api/signals/search?signal_type_slug=...

.. and - assuming the account has read permission on the ISNs - data is returned for all sites that have the medicine-supplies signal type.

