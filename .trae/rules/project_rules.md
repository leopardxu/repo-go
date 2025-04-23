1. 当前项目是使用golang重写google git-repo的项目;
2. 请尽量保持与google git-repo的功能一致，输出一直;
3. python 改成golang的过程中，请发挥golang语言的优势;
4. 请尽量使用golang的github.com/spf13/cobra 库;
5. 请尽量使用golang的并发编程，注意goroutine的泄露，使用pool来管理goroutine;
6. 终端log只需要输出重要log，不需要输出debug log，保持终端log清洁，如果需要debug log 请输出到指定log问题;
7. 请尽量使用golang的代码格式化工具，如gofmt, goimports, go vet, golint, staticcheck, etc;