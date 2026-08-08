import type { Person, PersonUpdateInput } from './types'

export function personToUpdateInput(person: Person): PersonUpdateInput {
  return {
    name: person.name,
    role: person.role,
    priority_weight: person.priority_weight,
    department: person.department,
    title: person.title,
    relation: person.relation,
    comm_style: person.comm_style,
    notes: person.notes,
    is_active: person.is_active,
  }
}
