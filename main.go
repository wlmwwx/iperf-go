package main

/*
	Possible Pitfalls:

	The big-endian/little-endian issue has not been considered, which may cause problems.
	Currently, the default is to use little-endian (following the system).
*/

func main() {
	test := newIperfTest()
	if test == nil {
		log.Error("create new test error")
	}
	test.init()

	if rtn := test.parseArguments(); rtn < 0 {
		log.Errorf("parse arguments error: %v", rtn)
	}

	if rtn := test.runTest(); rtn < 0 {
		log.Errorf("run test failed: %v", rtn)
	}

	test.freeTest()
}
