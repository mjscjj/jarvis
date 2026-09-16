import unittest
from gateway import command, MAIN, BOT

class GatewayBoundaryTests(unittest.TestCase):
    def test_private_user_capabilities_and_flag_injection_are_denied(self):
        cases = [
            ['im', '+messages-list', '--chat-id', 'chat'],
            ['--profile', MAIN, 'api', 'GET', '/open-apis/im/v1/messages', '--as', 'user'],
            ['--profile', BOT, 'api', 'GET', '/open-apis/auth/v3/tenant_access_token/internal', '--as', 'bot'],
            ['contact', '+search-user', '--query', '@private-file', '--as', 'user'],
            ['contact', '+search-user', '--query', 'person', '--as', 'user', '--output', 'private-file'],
            ['--profile', MAIN, 'config', 'show'],
            ['auth', 'login'],
        ]
        for args in cases:
            with self.subTest(args=args), self.assertRaises(ValueError):
                command(args)

    def test_directory_and_notification_commands_preserve_identity(self):
        result = command(['contact', '+search-user', '--query', 'person', '--as', 'user', '--format', 'json'])
        self.assertEqual(result[:4], ['--profile', MAIN, 'contact', '+search-user'])
        args = ['--profile', BOT, 'api', 'POST', '/open-apis/im/v1/messages', '--params', '{"receive_id_type":"email"}', '--data', '{"receive_id":"person@example.com","msg_type":"text","content":"{}"}', '--as', 'bot', '--format', 'json']
        self.assertEqual(command(args), args)

    def test_notification_bot_can_read_group_metadata_and_members(self):
        chat = command(['--profile', BOT, 'im', 'chats', 'get', '--chat-id', 'oc_fixed', '--user-id-type', 'open_id', '--as', 'bot'])
        self.assertEqual(chat, ['--profile', BOT, 'im', 'chats', 'get', '--chat-id', 'oc_fixed', '--user-id-type', 'open_id', '--as', 'bot', '--format', 'json'])
        members = command(['--profile', BOT, 'im', '+chat-members-list', '--chat-id', 'oc_fixed', '--member-types', 'bot', '--page-all', '--page-limit', '0', '--as', 'bot'])
        self.assertEqual(members, ['--profile', BOT, 'im', '+chat-members-list', '--chat-id', 'oc_fixed', '--member-types', 'bot', '--page-all', '--page-limit', '0', '--as', 'bot', '--format', 'json'])

    def test_group_reads_remain_bot_only_and_reject_file_options(self):
        for args in [
            ['--profile', BOT, 'im', 'chats', 'get', '--chat-id', 'oc_fixed', '--as', 'user'],
            ['--profile', BOT, 'im', '+chat-members-list', '--chat-id', 'oc_fixed', '--output', 'members.json', '--as', 'bot'],
        ]:
            with self.subTest(args=args), self.assertRaises(ValueError):
                command(args)

if __name__ == '__main__':
    unittest.main()

class DirectoryIdentityTest(unittest.TestCase):
    def test_exact_directory_read_routes(self):
        self.assertEqual(command(['--profile', MAIN, 'api', 'GET', '/open-apis/search/v1/user', '--params', '{"query":"person@example.test"}', '--as', 'user'])[:2], ['--profile', MAIN])
        self.assertEqual(command(['--profile', BOT, 'api', 'GET', '/open-apis/contact/v3/scopes', '--as', 'bot'])[:2], ['--profile', BOT])
        with self.assertRaises(ValueError):
            command(['--profile', BOT, 'contact', '+search-user', '--query', 'person', '--as', 'user'])
