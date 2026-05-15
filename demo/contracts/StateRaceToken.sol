// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

contract StateRaceToken {
    string public constant name = "State Race Token";
    string public constant symbol = "SRT";
    uint8 public constant decimals = 18;

    mapping(address => uint256) public balanceOf;
    mapping(address => mapping(address => uint256)) public allowance;

    event Transfer(address indexed from, address indexed to, uint256 value);
    event Approval(address indexed owner, address indexed spender, uint256 value);

    constructor(address initialHolder, uint256 initialSupply) {
        balanceOf[initialHolder] = initialSupply;
        emit Transfer(address(0), initialHolder, initialSupply);
    }

    function approve(address spender, uint256 value) external returns (bool) {
        allowance[msg.sender][spender] = value;
        emit Approval(msg.sender, spender, value);
        return true;
    }

    function transferFrom(address from, address to, uint256 value) external returns (bool) {
        uint256 allowed = allowance[from][msg.sender];
        require(allowed >= value, "ERC20: insufficient allowance");
        require(balanceOf[from] >= value, "ERC20: insufficient balance");

        allowance[from][msg.sender] = allowed - value;
        balanceOf[from] -= value;
        balanceOf[to] += value;

        emit Transfer(from, to, value);
        return true;
    }
}
