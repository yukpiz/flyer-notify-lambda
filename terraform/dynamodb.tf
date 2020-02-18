resource "aws_dynamodb_table" "flyer-notify-lambda-table" {
    name = "flyer-notify-lambda-table"
    read_capacity = 1
    write_capacity = 1
    hash_key = "id"
    attribute {
        name = "id"
        type = "S"
    }
}
