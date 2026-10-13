# schema-drift

[![CI](https://github.com/ahmed-hashim-pro/schema-drift/actions/workflows/ci.yml/badge.svg)](https://github.com/ahmed-hashim-pro/schema-drift/actions/workflows/ci.yml) [![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

A contract checker for event streams. It reads the JSON Schemas your
producers publish per topic, decides whether a new version breaks anyone
reading the old one (or the other way round), and blocks the pull request
before the change ships. Every "breaking" verdict comes with a concrete
event that proves it.

An event stream couples teams who never call each other. The orders team
changes `amount` from cents to a decimal string. Their tests pass, they
deploy, and three services downstream start failing to decode events at 2 am.
Nothing between the two teams was checked, because the only interface was a
topic name. Schema registries solve this for Avro on Kafka. JSON events on
NATS, SQS, webhooks or a plain Kafka topic usually have nothing. Here, the
contract is a directory in git, and the check is a CI step.

```
$ schema-drift check examples/contracts/orders/v2.json examples/contracts/orders/v3.json
v2 -> v3: NONE (breaking both ways)

backward: consumers on v3 reading events written with v2
  BREAKING /customer_id: v3 requires "customer_id"; v2 does not, so its events may leave it out
           e.g. {"order_id":"00000000-0000-4000-8000-000000000000","amount":0,"currency":"USD","created_at":"2024-01-01T00:00:00Z"}
  BREAKING /amount: v2 allows an integer; v3 does not (it allows string)
           e.g. {"order_id":"00000000-0000-4000-8000-000000000000","amount":0,"currency":"USD","created_at":"2024-01-01T00:00:00Z"}

forward: consumers on v2 reading events written with v3
  BREAKING /amount: v3 allows a string; v2 does not (it allows integer)
           e.g. {"order_id":"00000000-0000-4000-8000-000000000000","amount":"","currency":"USD","created_at":"2024-01-01T00:00:00Z","customer_id":"x"}

mode BACKWARD FAILS (consumers on the new schema must read events written with the old)
```

The `e.g.` lines aren't illustrations. Each is an event the checker built,
then confirmed with the validator: the first schema accepts it, the second
rejects it.

![Terminal recording: schema-drift check on orders v2 and v3 shows two backward and one forward breaking change, each with an example event; the gate demo then plays a pull request that edits a published version and adds two breaking versions, and fails with three problems; finally a v2 consumer crashes decoding a v3 event](docs/demo.gif)

## Quickstart

You need Go 1.22 or newer. There are no dependencies outside the standard
library.

```bash
git clone https://github.com/ahmed-hashim-pro/schema-drift && cd schema-drift
go build -o schema-drift ./cmd/schema-drift

./schema-drift check examples/contracts/orders/v2.json examples/contracts/orders/v3.json
./schema-drift history examples/contracts            # every topic, every version
./schema-drift verify examples/contracts/orders/v2.json examples/events/orders-mixed.ndjson
./schema-drift infer examples/events/orders-v2.ndjson # draft a schema from samples
SCHEMA_DRIFT=$PWD/schema-drift examples/gate-demo.sh  # a pull request, as CI sees it
```

`go test ./...` runs everything in about half a minute. `make check` adds
the race detector and gofmt, as CI does.

### In CI

Keep contracts in the repository, one folder per topic:

```
contracts/
  orders/
    mode        BACKWARD_TRANSITIVE   (optional; BACKWARD if absent)
    v1.json
    v2.json
  payments/
    mode        FULL
    v1.json
```

Then add one step to the pull request workflow:

```yaml
- uses: actions/checkout@v4
  with: { fetch-depth: 0 }      # the gate reads the base branch from git
- run: go install github.com/ahmed-hashim-pro/schema-drift/cmd/schema-drift@latest
- run: schema-drift gate --base origin/${{ github.base_ref }}
```

The gate compares the working tree with the base ref and fails (exit 1) on:

- **a new version that breaks the topic's mode**, against the previous
  version or, for `_TRANSITIVE` modes, against every earlier one
- **a published version edited in place**, which silently changes the
  contract under everyone already using it. Edits that change nothing,
  like rewording a description, pass.
- **a published version or topic deleted**
- **gaps in numbering**, and files that aren't `vN.json` or `mode`

Here is a pull request that makes three classic mistakes at once
(`examples/gate-demo.sh` builds the repository and runs it):

```
$ examples/gate-demo.sh
contracts: working tree compared with main
orders         BACKWARD_TRANSITIVE  v1..v3  2 problem(s)
payments       FULL                 v1..v2  1 problem(s)

orders v1 was published and has been changed in place; publish v4 instead
  consumers on v1 reading events written with v1 (edited):
    BREAKING /currency: v1 (edited) allows "GBP"; v1 does not
             e.g. {"order_id":"00000000-0000-4000-8000-000000000000","amount":0,"currency":"GBP","created_at":"2024-01-01T00:00:00Z"}

orders v3 breaks BACKWARD_TRANSITIVE: consumers on the new schema must read events written with the old
  consumers on v3 reading events written with v2:
    BREAKING /customer_id: v3 requires "customer_id"; v2 does not, so its events may leave it out
             e.g. {"order_id":"00000000-0000-4000-8000-000000000000","amount":0,"currency":"USD","created_at":"2024-01-01T00:00:00Z"}
    BREAKING /amount: v2 allows an integer; v3 does not (it allows string)
             e.g. {"order_id":"00000000-0000-4000-8000-000000000000","amount":0,"currency":"USD","created_at":"2024-01-01T00:00:00Z"}
  consumers on v3 reading events written with v1:
    BREAKING /customer_id: v3 requires "customer_id"; v1 does not, so its events may leave it out
             e.g. {"order_id":"00000000-0000-4000-8000-000000000000","amount":0,"currency":"USD","created_at":"2024-01-01T00:00:00Z"}
    BREAKING /amount: v1 allows an integer; v3 does not (it allows string)
             e.g. {"order_id":"00000000-0000-4000-8000-000000000000","amount":0,"currency":"USD","created_at":"2024-01-01T00:00:00Z"}
    BREAKING /currency: v1 allows "GBP"; v3 does not
             e.g. {"order_id":"00000000-0000-4000-8000-000000000000","amount":0,"currency":"GBP","created_at":"2024-01-01T00:00:00Z"}

payments v2 breaks FULL: old and new consumers must read both old and new events
  consumers on v1 reading events written with v2:
    BREAKING /status: v2 allows "refunded"; v1 does not
             e.g. {"payment_id":"00000000-0000-4000-8000-000000000000","order_id":"00000000-0000-4000-8000-000000000000","status":"refunded","amount":0}

3 problem(s)
```

## What "compatible" means

The modes are the ones in Confluent Schema Registry:

| Mode | Guarantee | Containment checked |
|---|---|---|
| `BACKWARD` (default) | consumers on the new schema can read events written with the old | old ⊆ new |
| `FORWARD` | consumers still on the old schema can read events written with the new | new ⊆ old |
| `FULL` | both | both |
| `*_TRANSITIVE` | the same, against every earlier version, not only the last | |
| `NONE` | no check | |

Which one fits depends on who upgrades first. If consumers deploy before
producers, you need `BACKWARD`. If producers ship first and consumers catch
up, you need `FORWARD`. If either can happen, or old events get replayed
from a retained topic, you need `FULL`.

Every mode reduces to one question: is every event valid under schema A also
valid under schema B? `schema-drift` answers it keyword by keyword:

- **types**: a writer allowing `number` against a reader allowing only
  `integer` is a fractional number away from a crash
- **enums**: values added or removed
- **numeric bounds and string lengths**, including the length a format
  implies (a uuid is exactly 36 characters; an email is at least 5)
- **formats**
- **required properties**
- **declared properties against `additionalProperties`**
- **array items and sizes**

It recurses through nested objects and arrays and reports each break at its
path, with `*` standing for "any item".

### Three classic mistakes, in `examples/contracts`

```
$ schema-drift history examples/contracts
orders         BACKWARD_TRANSITIVE  v1..v3  1 problem(s)
payments       FULL                 v1..v2  1 problem(s)
shipments      BACKWARD             v1..v2  1 problem(s)

orders v3 breaks BACKWARD_TRANSITIVE: consumers on the new schema must read events written with the old
  consumers on v3 reading events written with v2:
    BREAKING /customer_id: v3 requires "customer_id"; v2 does not, so its events may leave it out
             e.g. {"order_id":"00000000-0000-4000-8000-000000000000","amount":0,"currency":"USD","created_at":"2024-01-01T00:00:00Z"}
    BREAKING /amount: v2 allows an integer; v3 does not (it allows string)
             e.g. {"order_id":"00000000-0000-4000-8000-000000000000","amount":0,"currency":"USD","created_at":"2024-01-01T00:00:00Z"}
  consumers on v3 reading events written with v1:
    BREAKING /customer_id: v3 requires "customer_id"; v1 does not, so its events may leave it out
             e.g. {"order_id":"00000000-0000-4000-8000-000000000000","amount":0,"currency":"USD","created_at":"2024-01-01T00:00:00Z"}
    BREAKING /amount: v1 allows an integer; v3 does not (it allows string)
             e.g. {"order_id":"00000000-0000-4000-8000-000000000000","amount":0,"currency":"USD","created_at":"2024-01-01T00:00:00Z"}

payments v2 breaks FULL: old and new consumers must read both old and new events
  consumers on v1 reading events written with v2:
    BREAKING /status: v2 allows "refunded"; v1 does not
             e.g. {"payment_id":"00000000-0000-4000-8000-000000000000","order_id":"00000000-0000-4000-8000-000000000000","status":"refunded","amount":0}

shipments v2 breaks BACKWARD: consumers on the new schema must read events written with the old
  consumers on v2 reading events written with v1:
    BREAKING /carrier: v1 allows "carrier" with any value (additionalProperties); v2 constrains it
             e.g. {"shipment_id":"00000000-0000-4000-8000-000000000000","order_id":"00000000-0000-4000-8000-000000000000","shipped_at":"2024-01-01T00:00:00Z","carrier":""}

3 problem(s)
```

- **orders v3** changes `amount` from integer cents to a decimal string and
  makes `customer_id` required. Both break consumers: the first changes a
  type, and the second rejects every event already in the topic that lacks
  the field. `examples/consumer` is an ordinary Go consumer written against
  v2. It decodes v2 events fine and dies on the first v3 event with
  `json: cannot unmarshal string into Go struct field Order.amount of type int64`.
- **payments v2** adds a `refunded` status. That's backward compatible: new
  consumers read old events. It isn't forward compatible: a consumer still
  on v1 meets a status it has never heard of. Under `FULL`, that's a break.
  Adding an enum value is the most common breaking change people don't
  realise is one.
- **shipments v2** declares a `carrier` field on an *open* schema (no
  `additionalProperties: false`). It looks harmless. But v1 allowed *any*
  value under any undeclared name, so a v1 producer could legally send
  `"carrier": 7`, and v2 rejects it. With open schemas, every new
  constrained field breaks backward compatibility in principle. Confluent's
  JSON Schema support has the same issue. The practical answer is to close
  schemas (`additionalProperties: false`) and add fields as optional
  properties. That is backward compatible, and `orders v2` shows it passing.

## How the verdicts are checked

A compatibility checker can be wrong in two directions, and both are tested
against the validator as an independent oracle:

- **Calling a breaking change compatible.** This is the dangerous one.
  `TestSoundness` generates random schema pairs, some independent and most
  produced by mutating one into the other the way people edit contracts.
  For every pair judged compatible, it generates hundreds of events valid
  under the writer and checks each against the reader. Across three seed
  sets this came to about 38,600 compatible pairs and **7.6 million
  events: 0 counterexamples**. The default run covers about 12,900 pairs
  and 2.5 million events.
- **Calling a compatible change breaking.** This is the noisy one, and it
  teaches people to ignore the tool. Every reason the checker gives must
  carry a witness event: the writer accepts it, the reader rejects it, and
  the validator has to agree on both. `TestEveryBreakIsProven` fails on any
  unproven reason. Across the same seed sets that was about 44,700 reasons,
  **all proven**.

Getting there took real fixes, each found by these two tests:

- A writer whose only numbers were fractional skipped the range check.
  Seeded runs found a writer allowing exactly `2.5` judged compatible with
  a reader whose maximum is `0`.
- "Writer allows strings shorter than 2 characters" was reported against
  writers whose format makes that impossible. An email has at least 5
  characters, and a date-time at least 20.
- Writers whose constraints admit nothing at all (a `date` capped at 3
  characters) were reported as breaking, though they can never send a value.
- A reader property schema like `{"minLength": 0}` was treated as a
  constraint, when it constrains nothing.

The validator itself is tested on hand-built cases:
- JSON Pointers, including escaped `/` and `~`
- integer semantics (`1.0` is an integer)
- nullable type lists
- each format
- enum equality by value

`infer` is tested on 2,000 random samples. Every draft must accept its
sample and pass the strict parser.

## Strict about its input

A contract checker that skips a keyword it doesn't understand will call
changes compatible that aren't. So the parser fails closed. Each of these is
an error with a JSON Pointer, never ignored:

- a misspelt keyword, with a suggestion (`minimun` → `minimum`)
- a keyword outside the subset (`pattern`, `$ref`, `oneOf`, OpenAPI's
  `nullable`), with what to do instead
- a duplicate key, which `encoding/json` would silently resolve to the last
  value
- a schema no value can satisfy (`minimum` above `maximum`, a required
  property that `additionalProperties: false` forbids)
- an enum value the rest of the schema rejects

`verify` holds events to the same standard and reports each problem at its
pointer:

```
$ schema-drift verify examples/contracts/orders/v2.json examples/events/orders-mixed.ndjson
line 2: /order_id: "not-a-uuid" is not a valid uuid
line 2: /amount: -5 is less than the minimum 0
line 2: /currency: "GBP" is not one of ["USD","EUR"]
line 2: /created_at: "yesterday" is not a valid date-time
line 3: /coupon: property "coupon" is not allowed
line 4: /: duplicate key "amount"
line 5: /: required property "amount" is missing
5 events, 1 valid, 4 invalid
```

## The subset

Supported:

- `type` (one or a list; `["string","null"]` for nullable)
- `enum`
- `properties`, `required`, and `additionalProperties` (a boolean)
- `items` (one schema)
- `minimum` / `maximum`
- `minLength` / `maxLength`
- `minItems` / `maxItems`
- `format`: `date-time`, `date`, `uuid` and `email`. These are *asserted*,
  not just annotations as JSON Schema's default treats them.
- annotations: `title`, `description`, `$schema`, `$id`, `examples`

## What this does not do

- **No `$ref`, `oneOf`/`anyOf`/`allOf`, `pattern`, `const`, exclusive bounds
  or `multipleOf`.** Each is rejected with a hint, rather than ignored.
  Deciding containment exactly is easy for the supported keywords. It's hard
  for regular expressions and for unions of object shapes, and a checker
  that guessed there would be wrong in the dangerous direction.
- **Semantics are invisible.** If `amount` keeps type `integer` but changes
  from cents to dollars, every check passes. A schema can only carry what it
  states.
- **One schema per topic.** Topics that mix event types, discriminated by a
  field, need `oneOf`, which isn't supported. Split them, or use one schema
  with the union of fields.
- **No registry or broker integration.** The contracts live in git, and the
  gate reads git. It doesn't fetch schemas from Confluent Schema Registry or
  watch a live topic. `verify` checks samples you capture.
- **`infer` drafts, it doesn't know.** A field present in every sample is
  marked required, and the sample may simply not have shown it missing.

## Layout

```
schema/    the subset: strict parser, validator, formats
compat/    containment with witnesses; modes; the property tests
infer/     schema drafts from samples
gate/      contracts directories, from disk and from git; history and gate
cmd/       the CLI
examples/  contracts with a history of mistakes, events, a consumer, the gate demo
scripts/   docs.sh regenerates docs/*.txt, which this README quotes
```

## License

MIT
