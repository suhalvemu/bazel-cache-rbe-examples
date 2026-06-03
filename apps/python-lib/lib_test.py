import unittest
from lib import greet

class TestGreet(unittest.TestCase):
    def test_greet(self):
        self.assertEqual(greet("Bazel"), "Hello from Python, Bazel!")

if __name__ == "__main__":
    unittest.main()
