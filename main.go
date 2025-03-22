package main

/*
	可能存在的坑： 大端小端问题还没考虑，可能会出问题. 目前默认都是用小端（跟随系统）
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
