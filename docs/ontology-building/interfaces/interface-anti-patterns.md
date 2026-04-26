# Interface anti-patterns

Interfaces can become extremely powerful, but only if they remain semantically coherent.

## Anti-patterns

| Anti-pattern | Why it hurts | Better alternative |
| --- | --- | --- |
| giant catch-all interfaces | destroys reuse clarity | make interfaces narrowly purposeful |
| interfaces that duplicate one object type exactly | adds indirection with no reuse | keep logic on the object type unless reuse is real |
| using interfaces as loose tags only | misses semantic structure | give interfaces explicit properties and meaning |
| attaching too many unrelated interfaces | creates semantic confusion | compose only when behaviors truly align |

## Design rule

An interface should describe a reusable operational trait, not an arbitrary bucket of fields.
