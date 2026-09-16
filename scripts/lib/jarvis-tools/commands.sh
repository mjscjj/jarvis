# Thin command index: name, group, summary, handler. No flags or payload schemas.
register_command list-projects world 'List projects' cmd_list_projects
register_command get-project world 'Get one project' cmd_get_project
register_command create-project world 'Create a project from a JSON payload' cmd_create_project
register_command update-project world 'Replace a project'"'"'s control fields' cmd_update_project
register_command archive-project world 'Archive a project' cmd_archive_project
register_command list-key-matters world 'List open key matters' cmd_list_key_matters
register_command get-key-matter world 'Get one key matter' cmd_get_key_matter
register_command create-key-matter world 'Create a key matter from a JSON payload' cmd_create_key_matter
register_command update-key-matter world 'Replace a key matter'"'"'s control fields' cmd_update_key_matter
register_command touch-key-matter world 'Mark an open key matter as freshly active' cmd_touch_key_matter
register_command close-key-matter world 'Close a key matter' cmd_close_key_matter
register_command list-project-risks world 'List project risks' cmd_list_project_risks
register_command get-project-risk world 'Get one project risk' cmd_get_project_risk
register_command create-project-risk world 'Create a project risk from a JSON payload' cmd_create_project_risk
register_command update-project-risk world 'Replace a project risk control fields' cmd_update_project_risk
register_command close-project-risk world 'Close a project risk' cmd_close_project_risk
register_command list-project-changes world 'List project changes' cmd_list_project_changes
register_command get-project-change world 'Get one project change' cmd_get_project_change
register_command create-project-change world 'Create a project change from a JSON payload' cmd_create_project_change
register_command update-project-change world 'Replace project change control fields' cmd_update_project_change
register_command close-project-change world 'Close a project change' cmd_close_project_change
register_command list-groups world 'Search Feishu groups' cmd_list_groups
register_command get-group world 'Get one Feishu group by chat_id' cmd_get_group
register_command get-context world 'Assemble current principal/project/chat work context' cmd_get_context
register_command update-group world 'Replace one group'"'"'s control fields' cmd_update_group
register_command get-principal world 'Get the assistant owner'"'"'s profile' cmd_get_principal
register_command update-principal world 'Replace the principal'"'"'s control fields' cmd_update_principal
register_command list-persons world 'List important people, optionally by role' cmd_list_persons
register_command get-person world 'Get one person by Feishu open_id' cmd_get_person
register_command create-person world 'Create a person from a JSON payload' cmd_create_person
register_command update-person world 'Replace a person'"'"'s control fields' cmd_update_person
register_command delete-person world 'Delete a person' cmd_delete_person
register_command query-resources world 'Query managed resource summaries' cmd_query_resources
register_command get-resource world 'Get one managed resource' cmd_get_resource
register_command create-resource world 'Create a managed resource' cmd_create_resource
register_command update-resource world 'Replace a managed resource'"'"'s control fields' cmd_update_resource
register_command touch-resource world 'Mark an enabled managed resource as freshly active' cmd_touch_resource
register_command delete-resource world 'Delete a managed resource' cmd_delete_resource
register_command query-messages evidence 'Query locally captured Feishu messages' cmd_query_messages
register_command get-message evidence 'Load one locally captured message by database ID' cmd_get_message
register_command get-todo-event evidence 'Load one Todo lifecycle event by database ID' cmd_get_todo_event
register_command get-task-event evidence 'Load one Task lifecycle event by database ID' cmd_get_task_event
register_command query-captured-resources evidence 'List captured attachment/document summaries' cmd_query_captured_resources
register_command get-captured-resource evidence 'Load one captured resource including extracted text' cmd_get_captured_resource
register_command get-shared-memory memory 'Read shared memory' cmd_get_shared_memory
register_command set-shared-memory memory 'Replace shared memory' cmd_set_shared_memory
register_command list-skills skill 'List locally managed Skills' cmd_list_skills
register_command get-skill skill 'Read one locally managed Skill' cmd_get_skill
register_command append-shared-memory memory 'Append one entry to shared memory' cmd_append_shared_memory
register_command notice-principal notify 'Send an agent-authored Feishu card to the principal' cmd_notice_principal
register_command append-clue evidence 'Hand M2 one observed fact as evidence for M3' cmd_append_clue
register_command append-fact evidence 'Record one fact about a project, group, person or other subject' cmd_append_fact
register_command append-facts-batch evidence 'Record multiple facts from one JSON array' cmd_append_facts_batch
register_command list-facts evidence 'Read a subject'"'"'s recorded facts, optionally for one day' cmd_list_facts
register_command get-page world 'Read one entity'"'"'s long-term fact page' cmd_get_page
register_command get-page-guidance world 'Read the shared entity page content guidance' cmd_get_page_guidance
register_command update-page world 'Replace one entity'"'"'s long-term fact page' cmd_update_page
register_command list-pages world 'List long-term fact page indexes' cmd_list_pages
register_command list-backlinks world 'List pages that reference an entity' cmd_list_backlinks
register_command list-todos task 'List action clues, optionally for one day' cmd_list_todos
register_command get-todo task 'Get one clue with the details omitted from summaries' cmd_get_todo
register_command list-delegations task 'List identified commitments (independent of check Tasks)' cmd_list_delegations
register_command get-delegation task 'Read original context and current check result' cmd_get_delegation
register_command update-delegation task 'Update check result with an expected version' cmd_update_delegation
register_command list-delegation-tasks task 'List checks linked to one commitment' cmd_list_delegation_tasks
register_command list-tasks task 'List tasks, optionally for one day' cmd_list_tasks
register_command get-task task 'Read task overview or a frozen context section' cmd_get_task
register_command list-task-runs task 'Page through a task’s execution history' cmd_list_task_runs
register_command get-task-run task 'Read one run’s output and effects' cmd_get_task_run
register_command create-task task 'Create a normal manual or proactive Task for strong M5' cmd_create_task
register_command supplement-task task 'Add context to an existing Task without starting it' cmd_supplement_task
register_command resume-task task 'Answer a needs_human Task and resume its M5 session' cmd_resume_task
register_command start-task task 'Start one pending Task with strong M5' cmd_start_task
register_command update-task task 'Update one non-terminal Task' cmd_update_task
register_command close-task task 'Close one non-terminal Task' cmd_close_task
register_command list-scheduled-tasks schedule 'List time-triggered tasks' cmd_list_scheduled_tasks
register_command get-scheduled-task schedule 'Get one time-triggered task' cmd_get_scheduled_task
register_command create-scheduled-task schedule 'Create a time trigger for a new task' cmd_create_scheduled_task
register_command update-scheduled-task schedule 'Update a standalone time trigger' cmd_update_scheduled_task
register_command trigger-scheduled-task schedule 'Trigger a standalone time trigger now' cmd_trigger_scheduled_task
register_command set-todo-status task 'Park a clue, or hand it back for execution' cmd_set_todo_status
register_command yield-until schedule 'Pause the current task until a future time' cmd_yield_until
register_command delete-scheduled-task schedule 'Delete a standalone time trigger' cmd_delete_scheduled_task
register_command get-agent-identity world 'Read runtime display name and principal open_id' cmd_get_agent_identity

register_command get-world-overview world 'Read compact live world directory' cmd_get_world_overview
register_command list-relations world 'List cross-module entity relations' cmd_list_relations
register_command create-relation world 'Create or refresh one relation' cmd_create_relation
register_command delete-relation world 'Delete one relation' cmd_delete_relation
register_command purge-world-entities world 'Delete an explicit reviewed set of world entities' cmd_purge_world_entities
register_command get-world-progress world 'Read one persisted progress assessment' cmd_get_world_progress
register_command create-world-progress world 'Create a progress assessment' cmd_create_world_progress
register_command update-world-progress world 'Update a progress assessment' cmd_update_world_progress
register_command resolve-world-node world 'Read one world node from its owning module' cmd_resolve_world_node
register_command list-page-revisions world 'Read archived versions of an entity page' cmd_list_page_revisions
