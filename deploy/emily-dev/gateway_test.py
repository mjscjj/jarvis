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

if __name__ == '__main__':
    unittest.main()
